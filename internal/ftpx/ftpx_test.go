package ftpx

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"
)

// testDriver is an FTP server on a memory file system: the user "u" with the password "pw", or anonymous when it is allowed.
type testDriver struct {
	fs        afero.Fs
	tls       *tls.Config
	listener  net.Listener
	implicit  bool
	anonymous bool
}

func (d *testDriver) GetSettings() (*ftpserver.Settings, error) {
	s := &ftpserver.Settings{Listener: d.listener, PublicHost: "127.0.0.1"}
	if d.tls != nil && !d.implicit {
		s.TLSRequired = ftpserver.MandatoryEncryption
	}
	if d.implicit {
		s.TLSRequired = ftpserver.ImplicitEncryption
	}
	return s, nil
}
func (d *testDriver) ClientConnected(ftpserver.ClientContext) (string, error) { return "test", nil }
func (d *testDriver) ClientDisconnected(ftpserver.ClientContext)              {}
func (d *testDriver) GetTLSConfig() (*tls.Config, error) {
	if d.tls == nil {
		return nil, os.ErrInvalid
	}
	return d.tls, nil
}
func (d *testDriver) AuthUser(_ ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	if (user == "u" && pass == "pw") || (d.anonymous && user == "anonymous") {
		return d.fs, nil
	}
	return nil, os.ErrPermission
}

type serverOpts struct{ explicit, implicit, anonymous bool }

func startServer(t *testing.T, o serverOpts) (addr string, fs afero.Fs, roots *x509.CertPool) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	d := &testDriver{fs: afero.NewMemMapFs(), listener: ln, anonymous: o.anonymous, implicit: o.implicit}
	if o.explicit || o.implicit {
		d.tls, roots = selfSigned(t)
	}
	if o.implicit { // given a listener of ours, the library does not wrap it: TLS from the first byte is ours to do
		d.listener = tls.NewListener(ln, d.tls)
	}
	srv := ftpserver.NewFtpServer(d)
	go func() { _ = srv.ListenAndServe() }()
	t.Cleanup(func() { _ = srv.Stop() })
	return ln.Addr().String(), d.fs, roots
}

func selfSigned(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}, pool
}

func open(t *testing.T, cfg Config) *FS {
	t.Helper()
	f, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestBrowseAndChangeFiles(t *testing.T) {
	addr, mem, _ := startServer(t, serverOpts{})
	_ = afero.WriteFile(mem, "/hello.txt", []byte("hello"), 0o644)
	_ = mem.MkdirAll("/docs", 0o755)
	f := open(t, Config{Addr: addr, User: "u", Password: "pw"})
	if h, _ := f.Home(); h != "/" {
		t.Fatalf("home %q", h)
	}
	es, err := f.List("/")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range es {
		got[e.Name] = e.IsDir
	}
	if len(got) != 2 || got["docs"] != true || got["hello.txt"] != false {
		t.Fatalf("%+v", es)
	}
	if err := f.Mkdir("/new"); err != nil {
		t.Fatal(err)
	}
	if err := f.Rename("/hello.txt", "/docs/hi.txt"); err != nil {
		t.Fatal(err)
	}
	if st, err := f.Stat("/docs/hi.txt"); err != nil || st.Size != 5 || st.IsDir {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := f.Stat("/docs/none"); !os.IsNotExist(err) {
		t.Fatalf("a missing file: %v", err)
	}
	if err := f.Remove("/docs/hi.txt", false); err != nil {
		t.Fatal(err)
	}
	_ = afero.WriteFile(mem, "/new/a/b.txt", []byte("x"), 0o644)
	if err := f.Remove("/new", true); err != nil {
		t.Fatal(err)
	}
	if ok, _ := afero.Exists(mem, "/new"); ok {
		t.Fatal("the folder is still there")
	}
	if err := f.Chmod("/docs", 0o700); err == nil {
		t.Fatal("permissions cannot be changed over FTP")
	}
}

func TestUploadDownloadAndResume(t *testing.T) {
	addr, mem, _ := startServer(t, serverOpts{})
	f := open(t, Config{Addr: addr, User: "u", Password: "pw"})
	dir := t.TempDir()
	local := filepath.Join(dir, "a.bin")
	data := []byte(strings.Repeat("0123456789", 5000))
	_ = os.WriteFile(local, data, 0o644)

	var last int64
	if err := f.Upload(context.Background(), local, "/a.bin", false, func(done, total int64) { last = done; _ = total }); err != nil {
		t.Fatal(err)
	}
	if b, _ := afero.ReadFile(mem, "/a.bin"); string(b) != string(data) || last != int64(len(data)) {
		t.Fatalf("uploaded %d of %d", len(b), last)
	}
	if err := f.Upload(context.Background(), local, "/a.bin", false, nil); !os.IsExist(err) {
		t.Fatalf("a whole file is not sent again without overwrite: %v", err)
	}
	if err := f.Upload(context.Background(), local, "/a.bin", true, nil); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "b.bin")
	if err := f.Download(context.Background(), "/a.bin", out, false, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != string(data) {
		t.Fatalf("downloaded %d bytes", len(b))
	}
	// a partial local file is continued, not started again
	part := filepath.Join(dir, "c.bin")
	_ = os.WriteFile(part, data[:20000], 0o644)
	if err := f.Download(context.Background(), "/a.bin", part, false, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(part); string(b) != string(data) {
		t.Fatalf("resumed to %d bytes", len(b))
	}
	if err := f.Download(context.Background(), "/a.bin", out, false, nil); !os.IsExist(err) {
		t.Fatalf("a complete file is not overwritten: %v", err)
	}
}

func TestCancelStopsATransferAndKeepsWhatCame(t *testing.T) {
	addr, mem, _ := startServer(t, serverOpts{})
	f := open(t, Config{Addr: addr, User: "u", Password: "pw"})
	_ = afero.WriteFile(mem, "/big.bin", []byte(strings.Repeat("x", 4<<20)), 0o644)
	ctx, cancel := context.WithCancelCause(context.Background())
	out := filepath.Join(t.TempDir(), "big.bin")
	err := f.Download(ctx, "/big.bin", out, false, func(done, total int64) {
		if done > 64<<10 {
			cancel(context.Canceled)
		}
	})
	if err == nil {
		t.Fatal("the transfer was not stopped")
	}
	if st, e := os.Stat(out); e != nil || st.Size() == 0 || st.Size() >= 4<<20 {
		t.Fatalf("a partial file is kept: %v %v", st, e)
	}
}

func TestLoginErrorsAndAnonymous(t *testing.T) {
	addr, _, _ := startServer(t, serverOpts{anonymous: true})
	if _, err := Open(Config{Addr: addr, User: "u", Password: "wrong"}); err == nil {
		t.Fatal("a wrong password was accepted")
	}
	f := open(t, Config{Addr: addr}) // no user: anonymous
	if _, err := f.List("/"); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitAndImplicitTLSCheckTheCertificate(t *testing.T) {
	for _, mode := range []string{"explicit", "implicit"} {
		t.Run(mode, func(t *testing.T) {
			addr, mem, roots := startServer(t, serverOpts{explicit: mode == "explicit", implicit: mode == "implicit"})
			_ = afero.WriteFile(mem, "/s.txt", []byte("secret"), 0o644)
			// the system roots do not know this certificate: the connection is refused, the password is not sent
			if _, err := Open(Config{Addr: addr, Security: mode, User: "u", Password: "pw"}); err == nil || !strings.Contains(err.Error(), "certificate") {
				t.Fatalf("an untrusted certificate was accepted: %v", err)
			}
			f := open(t, Config{Addr: addr, Security: mode, User: "u", Password: "pw", TLS: &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}})
			es, err := f.List("/")
			if err != nil || len(es) != 1 || es[0].Name != "s.txt" {
				t.Fatalf("%+v %v", es, err)
			}
		})
	}
}

func TestUnknownSecurity(t *testing.T) {
	if _, err := Open(Config{Addr: "127.0.0.1:1", Security: "starttls"}); err == nil || !strings.Contains(err.Error(), "unknown FTP security") {
		t.Fatalf("%v", err)
	}
}
