package kubeapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// maxBody caps one answer: a list of every pod of a very large cluster is tens of megabytes, not more.
	maxBody = 64 << 20
	// requestTimeout is the time for one call that is not a stream.
	requestTimeout = 20 * time.Second
)

// Options are what the caller decides about a connection.
type Options struct {
	// ApproveExec says whether a login plugin that is not a well-known helper may run. nil: only the well-known ones run.
	ApproveExec func(ExecSpec) bool
	// Timeout replaces the default time for a call.
	Timeout time.Duration
}

// Client is one connection: one context of one kubeconfig. It is safe for concurrent use.
type Client struct {
	base    *url.URL
	http    *http.Client
	timeout time.Duration
	server  string
	user    User
	exec    *execSource
	tlsCfg  *tls.Config
	proxy   func(*http.Request) (*url.URL, error)
}

// APIError is an answer of the API server that is not a success. Message is the server's own sentence.
type APIError struct {
	Code    int
	Reason  string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("the server answered %d", e.Code)
}

// StatusCode is the HTTP status of the answer.
func (e *APIError) StatusCode() int { return e.Code }

// IsNotFound and the like let a caller treat a missing optional API (metrics-server, an Ingress class) as "not there".
func IsNotFound(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Code == http.StatusNotFound
}

// IsForbidden is true for an answer of 403.
func IsForbidden(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Code == http.StatusForbidden
}

// IsUnauthorized is true for an answer of 401: the credentials were not accepted.
func IsUnauthorized(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Code == http.StatusUnauthorized
}

// New opens a connection for one context of the kubeconfig. Nothing is sent yet.
func New(cfg *Config, ctxName string, opt Options) (*Client, error) {
	if err := cfg.Check(ctxName); err != nil {
		return nil, err
	}
	cx, _ := cfg.context(ctxName)
	cl, _ := cfg.cluster(cx.Cluster)
	u, _ := cfg.user(cx.User)
	base, _ := url.Parse(cl.Server)
	base.Path = strings.TrimRight(base.Path, "/")

	tc := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cl.Insecure, ServerName: cl.TLSServerName} //nolint:gosec // the file asks for it, and the screen says so
	if cl.CAData != "" || cl.CAFile != "" {
		pem, err := pemOf(cl.CAData, cl.CAFile)
		if err != nil {
			return nil, fmt.Errorf("certificate authority: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("the certificate authority of this kubeconfig holds no certificate")
		}
		tc.RootCAs = pool
	}
	if u.ClientCertData != "" || u.ClientCertFile != "" {
		certPEM, err := pemOf(u.ClientCertData, u.ClientCertFile)
		if err != nil {
			return nil, fmt.Errorf("client certificate: %w", err)
		}
		keyPEM, err := pemOf(u.ClientKeyData, u.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("client key: %w", err)
		}
		pair, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("the client certificate and key of this kubeconfig do not match: %w", err)
		}
		tc.Certificates = []tls.Certificate{pair}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = tc
	tr.Proxy = http.ProxyFromEnvironment
	if cl.ProxyURL != "" {
		p, _ := url.Parse(cl.ProxyURL)
		tr.Proxy = http.ProxyURL(p)
	}
	c := &Client{base: base, timeout: opt.Timeout, server: cl.Server, user: u, tlsCfg: tc, proxy: tr.Proxy,
		http: &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if c.timeout <= 0 {
		c.timeout = requestTimeout
	}
	if u.Exec != nil {
		spec := u.Exec.spec()
		if !spec.wellKnown() && (opt.ApproveExec == nil || !opt.ApproveExec(spec)) {
			return nil, &NeedsApprovalError{Spec: spec}
		}
		c.exec = &execSource{cfg: *u.Exec}
	}
	return c, nil
}

// NeedsApprovalError is returned by New when the login plugin is not one of the well-known helpers: the caller shows the command to
// the person, and calls New again with ApproveExec once they agree.
type NeedsApprovalError struct{ Spec ExecSpec }

// Command is the command line of the login program, to show to the person.
func (e *NeedsApprovalError) Command() string { return e.Spec.String() }

func (e *NeedsApprovalError) Error() string {
	return "this kubeconfig runs a program on this computer to sign in: " + e.Spec.String()
}

func pemOf(data, file string) ([]byte, error) {
	if data != "" {
		return decodeData(data)
	}
	if file == "" {
		return nil, errors.New("there is none")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Server is the address of the API server, as the kubeconfig gives it.
func (c *Client) Server() string { return c.server }

// TLSConfig is the TLS set-up of the connection, for a caller that opens a stream of its own (exec, port-forward).
func (c *Client) TLSConfig() *tls.Config { return c.tlsCfg.Clone() }

// URL is the full address of a path on the server.
func (c *Client) URL(path string) string { return c.base.String() + path }

// Header returns the headers that sign a request in: the token (asking the login plugin when there is one), or a password, and
// the impersonation headers when the file asks for them.
func (c *Client) Header(ctx context.Context) (http.Header, error) {
	h := http.Header{}
	h.Set("User-Agent", "vutty")
	switch {
	case c.exec != nil:
		tok, err := c.exec.token(ctx, false)
		if err != nil {
			return nil, err
		}
		h.Set("Authorization", "Bearer "+tok)
	case c.user.Token != "":
		h.Set("Authorization", "Bearer "+c.user.Token)
	case c.user.Username != "":
		req := &http.Request{Header: h}
		req.SetBasicAuth(c.user.Username, c.user.Password)
	}
	if c.user.As != "" {
		h.Set("Impersonate-User", c.user.As)
		for _, g := range c.user.AsGroups {
			h.Add("Impersonate-Group", g)
		}
	}
	return h, nil
}

func okPath(p string) error {
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, " \r\n\t\x00") {
		return errors.New("that is not a path of the API")
	}
	return nil
}

// Do makes one call and returns the body of a success. A failure is an *APIError when the server answered, else the network's error.
func (c *Client) Do(ctx context.Context, method, path, contentType string, body []byte) ([]byte, error) {
	out, _, err := c.do(ctx, method, path, contentType, body, maxBody)
	return out, err
}

// Capped is Do for what may be long (a log): the answer is cut at max bytes, and truncated says so.
func (c *Client) Capped(ctx context.Context, path string, max int64) (data []byte, truncated bool, err error) {
	return c.do(ctx, http.MethodGet, path, "", nil, max)
}

// Stream is a GET whose answer does not end (a log asked with follow=true): the body is copied to w as it arrives, until the server
// closes it, w refuses a write or ctx is cancelled. There is no time limit of its own: ctx is the limit. A refusal is an *APIError.
func (c *Client) Stream(ctx context.Context, path string, w io.Writer) error {
	if err := okPath(path); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL(path), nil)
	if err != nil {
		return err
	}
	h, err := c.Header(ctx)
	if err != nil {
		return err
	}
	req.Header = h
	req.Header.Set("Accept", "text/plain, */*")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return apiError(resp.StatusCode, b)
	}
	_, err = io.Copy(w, resp.Body)
	if ctx.Err() != nil {
		return nil // stopped by the caller
	}
	return err
}

// Get is Do with the method GET.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	return c.Do(ctx, http.MethodGet, path, "", nil)
}

func (c *Client) do(ctx context.Context, method, path, contentType string, body []byte, max int64) ([]byte, bool, error) {
	if err := okPath(path); err != nil {
		return nil, false, err
	}
	retried := false
	for {
		ctx2, cancel := context.WithTimeout(ctx, c.timeout)
		out, trunc, err := c.once(ctx2, method, path, contentType, body, max)
		cancel()
		// a login plugin's token can be revoked before it expires: ask for a new one, once
		if err != nil && IsUnauthorized(err) && c.exec != nil && !retried {
			retried = true
			if _, terr := c.exec.token(ctx, true); terr == nil {
				continue
			}
		}
		return out, trunc, err
	}
}

func (c *Client) once(ctx context.Context, method, path, contentType string, body []byte, max int64) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.URL(path), bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	h, err := c.Header(ctx)
	if err != nil {
		return nil, false, err
	}
	req.Header = h
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, false, err
	}
	trunc := int64(len(b)) > max
	if trunc {
		b = b[:max]
	}
	if resp.StatusCode/100 != 2 {
		return nil, false, apiError(resp.StatusCode, b)
	}
	return b, trunc, nil
}

// apiError reads the Status object the API server sends with a failure.
func apiError(code int, body []byte) error {
	e := &APIError{Code: code}
	var st struct {
		Message string `json:"message"`
		Reason  string `json:"reason"`
	}
	if json.Unmarshal(body, &st) == nil && st.Message != "" {
		e.Message, e.Reason = st.Message, st.Reason
		return e
	}
	if t := strings.TrimSpace(string(body)); t != "" && len(t) < 300 && !strings.HasPrefix(t, "<") {
		e.Message = t
		return e
	}
	switch code {
	case http.StatusUnauthorized:
		e.Message = "Unauthorized: the server did not accept the credentials"
	case http.StatusForbidden:
		e.Message = "Forbidden: the credentials may not do this"
	case http.StatusNotFound:
		e.Message = "the server has no such resource"
	}
	return e
}

// ---- login plugin ----

// execSource runs the login plugin of a user and keeps its token until shortly before it expires.
type execSource struct {
	cfg ExecConfig
	mu  sync.Mutex
	tok string
	exp time.Time // zero: no expiry was given
	at  time.Time
}

// execNoExpiry is how long a token without an expiry is reused.
const execNoExpiry = 5 * time.Minute

func (e *execSource) token(ctx context.Context, renew bool) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	if !renew && e.tok != "" {
		if e.exp.IsZero() && now.Sub(e.at) < execNoExpiry || !e.exp.IsZero() && now.Before(e.exp.Add(-30*time.Second)) {
			return e.tok, nil
		}
	}
	tok, exp, err := runExec(ctx, e.cfg)
	if err != nil {
		return "", err
	}
	e.tok, e.exp, e.at = tok, exp, now
	return tok, nil
}
