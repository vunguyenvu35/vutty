package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newKey(t *testing.T) (seed []byte, pub ed25519.PublicKey) {
	t.Helper()
	seed = make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		t.Fatal(err)
	}
	return seed, ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
}

func TestTheKeyBuiltIntoTheAppIsAValidPublicKey(t *testing.T) {
	pub, err := PublicKey()
	if err != nil || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("%v %d", err, len(pub))
	}
}

func TestSignAndVerifyRoundTripAndRefuseAnythingElse(t *testing.T) {
	seed, pub := newKey(t)
	msg := []byte("abc  vutty-1.3.0-macos.zip\n")
	text, err := Sign(seed, msg)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := DecodeSignature([]byte(text))
	if err != nil || !Verify(pub, msg, sig) {
		t.Fatalf("a good signature was refused: %v", err)
	}
	if Verify(pub, append([]byte("x"), msg...), sig) {
		t.Fatal("a changed message passed")
	}
	_, other := newKey(t)
	if Verify(other, msg, sig) {
		t.Fatal("another key passed")
	}
	bad := append([]byte(nil), sig...)
	bad[0] ^= 1
	if Verify(pub, msg, bad) {
		t.Fatal("a changed signature passed")
	}
	if Verify(nil, msg, sig) || Verify(pub, msg, nil) || Verify(pub, msg, sig[:10]) {
		t.Fatal("a missing or short key or signature passed")
	}
	for _, in := range []string{"", "not base64!!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := DecodeSignature([]byte(in)); err == nil {
			t.Errorf("%q was taken for a signature", in)
		}
	}
	if _, err := Sign([]byte("short"), msg); err == nil {
		t.Fatal("a short signing key was accepted")
	}
	if _, err := ParsePublicKey("nope"); err == nil {
		t.Fatal("a bad public key was accepted")
	}
}

func TestParseChecksums(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("B", 64)
	got, err := ParseChecksums([]byte(a + "  vutty-1.3.0-macos.zip\n" + b + " *vutty-1.3.0-linux-amd64.tar.gz\r\n\n"))
	if err != nil || got["vutty-1.3.0-macos.zip"] != a || got["vutty-1.3.0-linux-amd64.tar.gz"] != strings.ToLower(b) {
		t.Fatalf("%v %v", got, err)
	}
	for name, in := range map[string]string{
		"empty": "", "no name": a + "\n", "short sum": "abc  file\n", "path": a + "  ../evil\n", "slash": a + "  dir/file\n", "garbage": "hello\n",
		"twice differently": a + "  f\n" + strings.Repeat("c", 64) + "  f\n",
	} {
		if _, err := ParseChecksums([]byte(in)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := ParseChecksums([]byte(a + "  f\n" + a + "  f\n")); err != nil { // the same line twice is harmless
		t.Fatalf("a repeated identical line: %v", err)
	}
}

func TestGitHubHost(t *testing.T) {
	for h, want := range map[string]bool{
		"github.com": true, "api.github.com": true, "objects.githubusercontent.com": true, "release-assets.githubusercontent.com": true,
		"evil.com": false, "github.com.evil.com": false, "notgithub.com": false, "githubusercontent.com": false, "evilgithubusercontent.com": false, "": false,
	} {
		if GitHubHost(h) != want {
			t.Errorf("%q: want %v", h, want)
		}
	}
}

func local() *Client {
	return &Client{Insecure: true, Allow: func(h string) bool { return h == "127.0.0.1" }}
}

func TestDownloadHashesWhatArrivesAndReportsProgress(t *testing.T) {
	body := strings.Repeat("0123456789", 50000) // 500 KB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "f.bin")
	var last, calls int64
	sum, err := local().Download(context.Background(), srv.URL, dst, 10<<20, func(done, total int64) { last = done; calls++ })
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(body))
	if sum != hex.EncodeToString(want[:]) || last != int64(len(body)) || calls < 2 {
		t.Fatalf("sum %s last %d calls %d", sum, last, calls)
	}
	if st, _ := os.Stat(dst); st.Mode().Perm() != 0o600 || st.Size() != int64(len(body)) {
		t.Fatalf("%v", st)
	}
	if _, err := local().Download(context.Background(), srv.URL, dst, 10<<20, nil); err == nil { // never over a file that is there
		t.Fatal("an existing file was overwritten")
	}
}

func TestDownloadStopsAtTheLimitWhateverTheServerSays(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush() // no Content-Length: a stream that does not end
		for i := 0; i < 1000; i++ {
			if _, err := w.Write([]byte(strings.Repeat("x", 4096))); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	dst := filepath.Join(dir, "f.bin")
	if _, err := local().Download(context.Background(), srv.URL, dst, 100<<10, nil); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("a file over the limit was kept")
	}
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Length", "999999999"); w.Write([]byte("x")) }))
	defer big.Close()
	if _, err := local().Download(context.Background(), big.URL, filepath.Join(dir, "g.bin"), 1<<20, nil); err == nil {
		t.Fatal("a size the server announced over the limit was accepted")
	}
}

func TestFetchRefusesWhatIsTooBigOrNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			w.Write(make([]byte, 5000))
		case "/gone":
			http.NotFound(w, r)
		default:
			w.Write([]byte("hello"))
		}
	}))
	defer srv.Close()
	c := local()
	if b, err := c.Fetch(context.Background(), srv.URL+"/ok", 100); err != nil || string(b) != "hello" {
		t.Fatalf("%q %v", b, err)
	}
	if _, err := c.Fetch(context.Background(), srv.URL+"/big", 100); err == nil {
		t.Fatal("a file over the limit was cut and accepted")
	}
	if _, err := c.Fetch(context.Background(), srv.URL+"/gone", 100); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("%v", err)
	}
}

func TestRequestsStayOnAllowedHostsAndOverHTTPS(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("evil")) }))
	defer other.Close()
	// a server that is allowed but sends the client somewhere that is not
	hop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:"+strings.Split(other.URL, ":")[2]+"/x", http.StatusFound)
	}))
	defer hop.Close()
	c := local() // allows 127.0.0.1 only: "localhost" is another host
	if _, err := c.Fetch(context.Background(), hop.URL, 100); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("a redirect to another host was followed: %v", err)
	}
	if _, err := c.Fetch(context.Background(), "http://evil.example.com/a", 100); err == nil {
		t.Fatal("a host that is not allowed was fetched")
	}
	strict := &Client{} // the real rules: GitHub, over https
	for _, u := range []string{"http://github.com/a", "https://evil.example.com/a", "ftp://github.com/a", "file:///etc/passwd", "https://github.com.evil.com/a"} {
		if _, err := strict.Fetch(context.Background(), u, 100); err == nil || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("%s: %v", u, err)
		}
	}
}

func TestLatestReleaseReadsTagAndAssets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.Write([]byte("{not json"))
			return
		}
		if r.URL.Path == "/notag" {
			w.Write([]byte(`{}`))
			return
		}
		w.Write([]byte(`{"tag_name":"v1.3.0","assets":[{"name":"SHA256SUMS","browser_download_url":"https://github.com/x/SHA256SUMS","size":10},{"name":"a.zip","browser_download_url":"https://github.com/x/a.zip","size":99}]}`))
	}))
	defer srv.Close()
	rel, err := local().LatestRelease(context.Background(), srv.URL+"/ok")
	if err != nil || rel.Tag != "v1.3.0" || len(rel.Assets) != 2 {
		t.Fatalf("%+v %v", rel, err)
	}
	if a, ok := rel.Asset("a.zip"); !ok || a.Size != 99 || a.URL != "https://github.com/x/a.zip" {
		t.Fatalf("%+v", a)
	}
	if _, ok := rel.Asset("A.ZIP"); ok { // exact names only
		t.Fatal("a name matched in another case")
	}
	for _, p := range []string{"/bad", "/notag"} {
		if _, err := local().LatestRelease(context.Background(), srv.URL+p); err == nil {
			t.Errorf("%s was accepted", p)
		}
	}
}
