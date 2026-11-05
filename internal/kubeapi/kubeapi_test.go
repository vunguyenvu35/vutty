package kubeapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// kubeconfig builds the YAML of one context that talks to srv with the given user section (already indented under `user:`).
func kubeconfig(srv *httptest.Server, user string) []byte {
	ca := ""
	server := "https://127.0.0.1:1"
	if srv != nil {
		server = srv.URL
		ca = "    certificate-authority-data: " + base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})) + "\n"
	}
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: c1
clusters:
- name: cl
  cluster:
    server: %s
%s
contexts:
- name: c1
  context: {cluster: cl, user: u, namespace: shop}
users:
- name: u
  user:
%s
`, server, ca, user))
}

func open(t *testing.T, cfg []byte, opt Options) *Client {
	t.Helper()
	c, err := Parse(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cl, err := New(c, "c1", opt)
	if err != nil {
		t.Fatal(err)
	}
	return cl
}

func TestTokenIsSentAndTheCertificateAuthorityIsTrusted(t *testing.T) {
	var got string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()
	cl := open(t, kubeconfig(srv, "    token: s3cret"), Options{})
	out, err := cl.Get(context.Background(), "/api/v1/pods")
	if err != nil || string(out) != `{"items":[]}` {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got != "Bearer s3cret" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestAServerThatTheFileDoesNotTrustIsRefused(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	cfg := kubeconfig(nil, "    token: x")
	cfg = []byte(strings.Replace(string(cfg), "https://127.0.0.1:1", srv.URL, 1)) // no certificate-authority: the system roots do not know this one
	cl := open(t, cfg, Options{})
	if _, err := cl.Get(context.Background(), "/version"); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("want a certificate error, got %v", err)
	}
}

func TestInsecureSkipVerifyIsHonoured(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{}")) }))
	defer srv.Close()
	cfg := strings.Replace(string(kubeconfig(nil, "    token: x")), "https://127.0.0.1:1\n", srv.URL+"\n    insecure-skip-tls-verify: true\n", 1)
	cl := open(t, []byte(cfg), Options{})
	if _, err := cl.Get(context.Background(), "/version"); err != nil {
		t.Fatal(err)
	}
	c, _ := Parse([]byte(cfg))
	if !c.List()[0].Insecure {
		t.Fatal("the screen must be able to say that verification is off")
	}
}

func TestAPIErrorUsesTheServersSentence(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"kind":"Status","reason":"Forbidden","message":"pods is forbidden: User \"dev\" cannot list resource \"pods\" in namespace \"kube-system\""}`))
	}))
	defer srv.Close()
	cl := open(t, kubeconfig(srv, "    token: x"), Options{})
	_, err := cl.Get(context.Background(), "/api/v1/namespaces/kube-system/pods")
	if !IsForbidden(err) || !strings.Contains(err.Error(), "cannot list resource") {
		t.Fatalf("err = %v", err)
	}
	if IsNotFound(err) || IsUnauthorized(err) {
		t.Fatal("wrong class")
	}
}

func TestPathsThatCouldSmuggleAHeaderAreRefused(t *testing.T) {
	cl := open(t, kubeconfig(nil, "    token: x"), Options{})
	for _, p := range []string{"api/v1", "/api/v1\r\nX: y", "/a b", ""} {
		if _, err := cl.Get(context.Background(), p); err == nil || !strings.Contains(err.Error(), "not a path") {
			t.Errorf("%q: err = %v", p, err)
		}
	}
}

func TestAnswerIsCappedAndTruncationIsReported(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 1000)))
	}))
	defer srv.Close()
	cl := open(t, kubeconfig(srv, "    token: x"), Options{})
	b, trunc, err := cl.Capped(context.Background(), "/log", 100)
	if err != nil || !trunc || len(b) != 100 {
		t.Fatalf("len=%d trunc=%v err=%v", len(b), trunc, err)
	}
}

func TestARedirectIsNotFollowed(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			hits.Add(1)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound) // the token must not follow a redirect to another place
	}))
	defer srv.Close()
	cl := open(t, kubeconfig(srv, "    token: x"), Options{})
	if _, err := cl.Get(context.Background(), "/api"); err == nil {
		t.Fatal("a redirect must be an error")
	}
	if hits.Load() != 0 {
		t.Fatal("the redirect was followed")
	}
}

func TestPathPrefixOfTheServerIsKept(t *testing.T) { // Rancher: https://host/k8s/clusters/c-abc
	var path string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { path = r.URL.Path; _, _ = w.Write([]byte("{}")) }))
	defer srv.Close()
	cfg := strings.Replace(string(kubeconfig(srv, "    token: x")), srv.URL, srv.URL+"/k8s/clusters/c-abc/", 1)
	if _, err := open(t, []byte(cfg), Options{}).Get(context.Background(), "/api/v1/nodes"); err != nil {
		t.Fatal(err)
	}
	if path != "/k8s/clusters/c-abc/api/v1/nodes" {
		t.Fatalf("path = %q", path)
	}
}

func TestImpersonationHeaders(t *testing.T) {
	var user, group string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, group = r.Header.Get("Impersonate-User"), r.Header.Get("Impersonate-Group")
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	cl := open(t, kubeconfig(srv, "    token: x\n    as: jane\n    as-groups: [devs]"), Options{})
	if _, err := cl.Get(context.Background(), "/version"); err != nil {
		t.Fatal(err)
	}
	if user != "jane" || group != "devs" {
		t.Fatalf("user=%q group=%q", user, group)
	}
}

func TestBasicAuth(t *testing.T) {
	var u, p string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { u, p, _ = r.BasicAuth(); _, _ = w.Write([]byte("{}")) }))
	defer srv.Close()
	if _, err := open(t, kubeconfig(srv, "    username: ann\n    password: pw"), Options{}).Get(context.Background(), "/version"); err != nil {
		t.Fatal(err)
	}
	if u != "ann" || p != "pw" {
		t.Fatalf("%q %q", u, p)
	}
}

func TestClientCertificateIsPresented(t *testing.T) {
	var cn string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) > 0 {
			cn = r.TLS.PeerCertificates[0].Subject.CommonName
		}
		_, _ = w.Write([]byte("{}"))
	}))
	srv.StartTLS()
	defer srv.Close()
	// the test server asks for a client certificate and accepts any
	srv.TLS.ClientAuth = tls.RequireAnyClientCert
	cert, key := selfSigned(t, "dev-user")
	user := "    client-certificate-data: " + base64.StdEncoding.EncodeToString(cert) + "\n    client-key-data: " + base64.StdEncoding.EncodeToString(key)
	if _, err := open(t, kubeconfig(srv, user), Options{}).Get(context.Background(), "/version"); err != nil {
		t.Fatal(err)
	}
	if cn != "dev-user" {
		t.Fatalf("the server saw %q", cn)
	}
}

// ---- what is refused when a kubeconfig is imported ----

func TestCheckRefusesWhatCouldSendALocalFile(t *testing.T) {
	cases := map[string]string{
		"tokenFile":     "    tokenFile: /Users/me/.ssh/id_rsa",
		"auth-provider": "    auth-provider: {name: gcp}",
		"relative path": "    client-certificate: certs/me.crt\n    client-key: certs/me.key",
		"empty command": "    exec: {apiVersion: client.authentication.k8s.io/v1, command: ''}",
	}
	for name, user := range cases {
		c, err := Parse(kubeconfig(nil, user))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Check("c1"); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
	c, _ := Parse(kubeconfig(nil, "    token: x"))
	if err := c.Check("c1"); err != nil {
		t.Fatalf("a plain token must pass: %v", err)
	}
	if err := c.Check("nope"); err == nil {
		t.Fatal("unknown context")
	}
}

func TestCheckRefusesAServerThatIsNotAnAddress(t *testing.T) {
	for _, s := range []string{"file:///etc/passwd", "ftp://x", "https://", "x y"} {
		c, err := Parse([]byte(strings.Replace(string(kubeconfig(nil, "    token: x")), "https://127.0.0.1:1", s, 1)))
		if err != nil {
			continue
		}
		if c.Check("c1") == nil {
			t.Errorf("%q passed", s)
		}
	}
}

func TestParseRefusesWhatIsNotAKubeconfig(t *testing.T) {
	for name, data := range map[string][]byte{"empty": nil, "text": []byte("hello"), "no contexts": []byte("apiVersion: v1\nkind: Config\n"), "huge": make([]byte, MaxConfigSize+1)} {
		if _, err := Parse(data); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestListCarriesNoSecretButShowsTheLoginCommand(t *testing.T) {
	c, _ := Parse(kubeconfig(nil, "    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: aws\n      args: [eks, get-token, --cluster-name, prod]"))
	l := c.List()
	if len(l) != 1 || l[0].Auth != "exec" || l[0].Exec != "aws eks get-token --cluster-name prod" || !l[0].Current || l[0].Namespace != "shop" {
		t.Fatalf("%+v", l)
	}
	c, _ = Parse(kubeconfig(nil, "    token: topsecret"))
	if fmt.Sprintf("%+v", c.List()) == "" || strings.Contains(fmt.Sprintf("%+v", c.List()), "topsecret") {
		t.Fatal("the list must not carry the token")
	}
}

// ---- login plugin ----

func TestWellKnownLoginHelpersRunWithoutAsking(t *testing.T) {
	for _, c := range []string{"aws", "/opt/homebrew/bin/aws", "gke-gcloud-auth-plugin", "kubelogin", `C:\Program Files\Amazon\AWSCLIV2\aws.exe`, "AZ"} {
		if !wellKnownExec(c) {
			t.Errorf("%s should be well known", c)
		}
	}
	for _, c := range []string{"sh", "/tmp/get-token", "bash", "curl", "python3", "awsx"} {
		if wellKnownExec(c) {
			t.Errorf("%s must ask first", c)
		}
	}
}

func TestAnUnknownLoginProgramNeedsApprovalAndNeverRunsWithoutIt(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := fakePlugin(t, dir, `touch `+marker+`; echo '{"status":{"token":"t"}}'`)
	c, _ := Parse(kubeconfig(nil, "    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: "+script))
	_, err := New(c, "c1", Options{})
	var na *NeedsApprovalError
	if !errors.As(err, &na) || na.Spec.Command != script {
		t.Fatalf("err = %v", err)
	}
	if _, serr := os.Stat(marker); serr == nil {
		t.Fatal("the program ran before it was approved")
	}
	if _, err := New(c, "c1", Options{ApproveExec: func(e ExecSpec) bool { return e.Command == "something else" }}); err == nil {
		t.Fatal("an approval of another command must not count")
	}
	if _, err := New(c, "c1", Options{ApproveExec: func(e ExecSpec) bool { return e.String() == script }}); err != nil {
		t.Fatal(err)
	}
}

func TestLoginPluginTokenIsUsedCachedAndRenewedAfter401(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	script := fakePlugin(t, dir, `echo x >> `+count+`; n=$(wc -l < `+count+` | tr -d ' ');
echo "{\"status\":{\"token\":\"tok$n\",\"expirationTimestamp\":\"`+time.Now().Add(time.Hour).UTC().Format(time.RFC3339)+`\"}}"`)
	var seen []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := r.Header.Get("Authorization")
		seen = append(seen, a)
		if a == "Bearer tok1" && len(seen) > 2 { // the first token is revoked after two good calls
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"message":"Unauthorized"}`))
			return
		}
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	approve := func(ExecSpec) bool { return true }
	cl := open(t, kubeconfig(srv, "    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: "+script), Options{ApproveExec: approve})
	for i := 0; i < 3; i++ {
		if _, err := cl.Get(context.Background(), "/version"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	b, _ := os.ReadFile(count)
	if n := strings.Count(string(b), "x"); n != 2 { // once at first, once after the 401; the second call reused the first token
		t.Fatalf("the plugin ran %d times (calls saw %v)", n, seen)
	}
	if seen[len(seen)-1] != "Bearer tok2" {
		t.Fatalf("the retry did not use the new token: %v", seen)
	}
}

func TestLoginPluginFailureShowsItsOwnMessage(t *testing.T) {
	dir := t.TempDir()
	script := fakePlugin(t, dir, `echo "Unable to locate credentials. You can configure credentials by running aws configure." >&2; exit 255`)
	cl := open(t, kubeconfig(nil, "    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: "+script), Options{ApproveExec: func(ExecSpec) bool { return true }})
	_, err := cl.Get(context.Background(), "/version")
	if err == nil || !strings.Contains(err.Error(), "Unable to locate credentials") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginPluginThatAnswersWithoutATokenIsAnError(t *testing.T) {
	dir := t.TempDir()
	script := fakePlugin(t, dir, `echo '{"status":{}}'`)
	cl := open(t, kubeconfig(nil, "    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: "+script), Options{ApproveExec: func(ExecSpec) bool { return true }})
	if _, err := cl.Get(context.Background(), "/version"); err == nil || !strings.Contains(err.Error(), "no token") {
		t.Fatalf("err = %v", err)
	}
}

func TestMissingLoginProgramSaysWhat(t *testing.T) {
	c, _ := Parse(kubeconfig(nil, "    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: aws-not-installed-here"))
	cl, err := New(c, "c1", Options{ApproveExec: func(ExecSpec) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cl.Get(context.Background(), "/version"); err == nil || !strings.Contains(err.Error(), "is not installed") {
		t.Fatalf("err = %v", err)
	}
}

// ---- helpers ----

func fakePlugin(t *testing.T, dir, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake login plugin is a shell script")
	}
	p := filepath.Join(dir, "get-token")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func selfSigned(t *testing.T, cn string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
}

// A name is not enough: the same name in a folder anybody can write, or on a network share, is not the helper.
func TestAWellKnownNameInAFolderAnybodyCanWriteAsksFirst(t *testing.T) {
	for _, c := range []string{"/tmp/evil/aws", "./aws", "aws/../../tmp/aws", `\\evil\share\aws.exe`, `C:\Users\x\Downloads\kubectl.exe`,
		"/Users/x/Downloads/gcloud", "/usr/local/bin/../../../tmp/aws", `C:\Program Files\..\Users\x\aws.exe`, "relative/dir/az"} {
		if wellKnownExec(c) {
			t.Errorf("%s ran without asking", c)
		}
	}
	for _, c := range []string{"aws", "/usr/local/bin/aws", "/usr/bin/gcloud", `C:\Program Files\Amazon\AWSCLIV2\aws.exe`, `c:\program files (x86)\Google\Cloud SDK\gcloud.cmd`} {
		if !wellKnownExec(c) {
			t.Errorf("%s should be well known", c)
		}
	}
}

// What the file sets for the program can change what runs: that asks first, and is part of what is approved.
func TestAHelperWithAVariableThatChangesWhatRunsAsksFirstAndTheVariableIsPartOfTheApproval(t *testing.T) {
	cfg := func(env string) *Config {
		c, err := Parse(kubeconfig(nil, "    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: aws\n      args: [eks, get-token]\n"+env))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if _, err := New(cfg("      env:\n        - {name: AWS_PROFILE, value: prod}\n        - {name: AWS_REGION, value: eu-1}\n"), "c1", Options{}); err != nil {
		var na *NeedsApprovalError
		if errors.As(err, &na) {
			t.Fatalf("a profile and a region must not ask: %v", err)
		}
	}
	for _, name := range []string{"LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "PATH", "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "CLOUDSDK_PYTHON", "PYTHONSTARTUP", "BASH_ENV", "HOME", "NODE_OPTIONS"} {
		_, err := New(cfg("      env:\n        - {name: "+name+", value: /tmp/x}\n"), "c1", Options{})
		var na *NeedsApprovalError
		if !errors.As(err, &na) {
			t.Errorf("%s: err = %v", name, err)
			continue
		}
		if !strings.Contains(na.Command(), name+"=/tmp/x") || !strings.Contains(na.Command(), "aws eks get-token") {
			t.Errorf("%s: the person must see the variable: %q", name, na.Command())
		}
		// approving the same program without the variable does not approve it with the variable
		if _, err := New(cfg("      env:\n        - {name: "+name+", value: /tmp/x}\n"), "c1", Options{ApproveExec: func(e ExecSpec) bool { return e.String() == "aws eks get-token" }}); err == nil {
			t.Errorf("%s: approved by the command without the variable", name)
		}
	}
}

func TestALoginProgramGivenAsARelativePathIsRefusedEvenWhenApproved(t *testing.T) {
	if _, err := lookup("./get-token"); err == nil || !strings.Contains(err.Error(), "full path") {
		t.Fatalf("%v", err)
	}
	if _, err := lookup("bin/get-token"); err == nil {
		t.Fatal("a relative path was accepted")
	}
	if p, err := lookup("/opt/x/get-token"); err != nil || p != "/opt/x/get-token" {
		t.Fatalf("%q %v", p, err)
	}
}
