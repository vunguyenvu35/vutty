// Package updater holds what a self-update trusts and how it fetches: a signature check against a key built into the app, the
// checksum list of a release, and downloads that stay on GitHub, are capped in size and are hashed as they arrive. It has no
// idea what an app bundle is: replacing the running program is the app's job.
package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// Limits: a checksum list and a release description are small; the application itself is the only big download.
const (
	MaxSmallFile = 1 << 20
	MaxAppFile   = 400 << 20
)

// PublicKey returns the key a release has to be signed with.
func PublicKey() (ed25519.PublicKey, error) { return ParsePublicKey(PublicKeyBase64) }

// ParsePublicKey reads a base64 ed25519 public key.
func ParsePublicKey(b64 string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("this build has no valid update key")
	}
	return ed25519.PublicKey(raw), nil
}

// DecodeSignature reads the .sig file next to a signed file: the 64 bytes of an ed25519 signature in base64.
func DecodeSignature(text []byte) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(text)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return nil, errors.New("the signature file is not a signature")
	}
	return raw, nil
}

// Verify reports whether sig is a signature of msg by pub.
func Verify(pub ed25519.PublicKey, msg, sig []byte) bool {
	return len(pub) == ed25519.PublicKeySize && len(sig) == ed25519.SignatureSize && ed25519.Verify(pub, msg, sig)
}

// Sign makes the signature of msg, in the form DecodeSignature reads. seed is the 32-byte private key.
func Sign(seed []byte, msg []byte) (string, error) {
	if len(seed) != ed25519.SeedSize {
		return "", errors.New("the signing key must be 32 bytes")
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), msg)) + "\n", nil
}

var sumLine = regexp.MustCompile(`^([0-9a-fA-F]{64}) [ *]?([^\s/\\][^/\\]*)$`)

// ParseChecksums reads the output of sha256sum: one "<64 hex>  <file name>" per line. A name that appears twice with two
// different sums is an error (a list somebody added to).
func ParseChecksums(text []byte) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(string(text), "\n") {
		line = strings.TrimRight(line, "\r ")
		if line == "" {
			continue
		}
		m := sumLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("the checksum list has a line that is not a checksum")
		}
		sum := strings.ToLower(m[1])
		if prev, ok := out[m[2]]; ok && prev != sum {
			return nil, fmt.Errorf("the checksum list names %s twice with different sums", m[2])
		}
		out[m[2]] = sum
	}
	if len(out) == 0 {
		return nil, errors.New("the checksum list is empty")
	}
	return out, nil
}

// Asset is one file of a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is what the GitHub API says about one release.
type Release struct {
	Tag        string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	Assets     []Asset `json:"assets"`
}

// Asset returns the asset with this exact name.
func (r Release) Asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// GitHubHost says whether a download may come from this host: GitHub itself and the hosts GitHub serves release files from.
func GitHubHost(host string) bool {
	host = strings.ToLower(host)
	return host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

// Client fetches release data and files. Every request, and every redirect it is sent on, must go to an allowed host over https
// (Allow and Insecure are for tests that talk to a local stand-in).
type Client struct {
	HTTP     *http.Client
	Allow    func(host string) bool
	Insecure bool
	UA       string
}

func (c *Client) http() *http.Client {
	base := c.HTTP
	if base == nil {
		base = &http.Client{}
	}
	cp := *base
	cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return c.check(req.URL)
	}
	return &cp
}

func (c *Client) check(u *url.URL) error {
	if u.Scheme != "https" && !(c.Insecure && u.Scheme == "http") {
		return fmt.Errorf("refusing to fetch %s over %s", u.Host, u.Scheme)
	}
	allow := c.Allow
	if allow == nil {
		allow = GitHubHost
	}
	if !allow(u.Hostname()) {
		return fmt.Errorf("refusing to fetch from %s", u.Host)
	}
	return nil
}

func (c *Client) get(ctx context.Context, rawURL, accept string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if err := c.check(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("User-Agent", firstNonEmpty(c.UA, "vutty-updater"))
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", u.Host, errors.Unwrap(err))
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s answered %s", u.Host, resp.Status)
	}
	return resp, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Fetch reads a small file (at most max bytes: more is an error, not a cut-off).
func (c *Client) Fetch(ctx context.Context, rawURL string, max int64) ([]byte, error) {
	resp, err := c.get(ctx, rawURL, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("the file is larger than %d bytes", max)
	}
	return b, nil
}

// LatestRelease reads the release the URL describes (the "latest" endpoint of the GitHub API).
func (c *Client) LatestRelease(ctx context.Context, apiURL string) (Release, error) {
	resp, err := c.get(ctx, apiURL, "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	var rel Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, MaxSmallFile)).Decode(&rel); err != nil {
		return Release{}, errors.New("unexpected answer from GitHub")
	}
	if rel.Tag == "" {
		return Release{}, errors.New("unexpected answer from GitHub")
	}
	return rel, nil
}

// Download saves a file to dst (created new, mode 0600) while hashing it, and returns the SHA-256 in hex. It stops with an error
// when the file is larger than max, whatever the server said it would be. progress is told how many bytes are in so far and how
// many to expect (0 when the server does not say).
func (c *Client) Download(ctx context.Context, rawURL, dst string, max int64, progress func(done, total int64)) (string, error) {
	resp, err := c.get(ctx, rawURL, "application/octet-stream")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.ContentLength > max {
		return "", fmt.Errorf("the file is larger than %d MB", max>>20)
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	var done int64
	buf := make([]byte, 128<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > max {
				f.Close()
				os.Remove(dst)
				return "", fmt.Errorf("the file is larger than %d MB", max>>20)
			}
			h.Write(buf[:n])
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(dst)
				return "", werr
			}
			if progress != nil {
				progress(done, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(dst)
			return "", rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(dst)
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
