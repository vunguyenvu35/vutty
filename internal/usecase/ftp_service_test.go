package usecase

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vutty/internal/domain"
)

// fakeFTPFS is a RemoteFS that lists one file and answers Ping, to see what the service hands to the FTP opener.
type fakeFTPFS struct {
	RemoteFS
	pings  atomic.Int32
	failAt int32
	closed atomic.Bool
}

func (f *fakeFTPFS) Home() (string, error) { return "/home/u", nil }
func (f *fakeFTPFS) List(dir string) ([]domain.RemoteEntry, error) {
	return []domain.RemoteEntry{{Name: "a.txt", Path: dir + "/a.txt", Size: 3}}, nil
}
func (f *fakeFTPFS) Close() error { f.closed.Store(true); return nil }
func (f *fakeFTPFS) Ping() error {
	if n := f.pings.Add(1); f.failAt > 0 && n >= f.failAt {
		return errors.New("connection reset")
	}
	return nil
}

func ftpHost(t *testing.T, h *harness, mod func(*domain.Host)) domain.Host {
	t.Helper()
	x := domain.Host{Label: "files", Hostname: "ftp.example.com", Username: "web", FTPEnabled: true}
	if mod != nil {
		mod(&x)
	}
	saved, err := h.app.Host.SaveHost(x)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestFTPHostRules(t *testing.T) {
	h := newHarness(t).unlocked(t)
	got := ftpHost(t, h, nil)
	if got.FTPPort != 21 || got.FTPSecurity != "" {
		t.Fatalf("%+v", got)
	}
	got = ftpHost(t, h, func(x *domain.Host) { x.Label = "tls"; x.FTPSecurity = "Implicit" })
	if got.FTPPort != 990 || got.FTPSecurity != "implicit" {
		t.Fatalf("%+v", got)
	}
	off := ftpHost(t, h, func(x *domain.Host) {
		x.Label = "off"
		x.FTPEnabled = false
		x.FTPPort = 2121
		x.FTPSecurity = "explicit"
	})
	if off.FTPPort != 0 || off.FTPSecurity != "" {
		t.Fatalf("a port or a mode without FTP is dropped: %+v", off)
	}
	for _, bad := range []func(*domain.Host){
		func(x *domain.Host) { x.FTPSecurity = "starttls" },
		func(x *domain.Host) { x.FTPPort = 70000 },
		func(x *domain.Host) { x.FTPUsername = "a\r\nDELE x" },
	} {
		x := domain.Host{Label: "bad", Hostname: "h", FTPEnabled: true}
		bad(&x)
		if _, err := h.app.Host.SaveHost(x); err == nil {
			t.Errorf("%+v was accepted", x)
		}
	}
}

func TestOpenFTPUsesTheLoginAndPasswordOfTheHost(t *testing.T) {
	h := newHarness(t).unlocked(t)
	var seen FTPConfig
	fs := &fakeFTPFS{}
	h.core.OpenFTP = func(cfg FTPConfig) (RemoteFS, error) { seen = cfg; return fs, nil }
	host := ftpHost(t, h, func(x *domain.Host) { x.FTPSecurity = "explicit"; x.FTPUsername = "ftpuser" })
	if err := h.app.Host.SetFTPPassword(host.ID, "s3cret"); err != nil {
		t.Fatal(err)
	}
	info, err := h.app.Sftp.Open(FTPID(host.ID), "k")
	if err != nil {
		t.Fatal(err)
	}
	if seen != (FTPConfig{Addr: "ftp.example.com:21", Security: "explicit", User: "ftpuser", Password: "s3cret"}) {
		t.Fatalf("%+v", seen)
	}
	if info.Home != "/home/u" || info.Host != "files" {
		t.Fatalf("%+v", info)
	}
	es, err := h.app.Sftp.List(info.SID, "/home/u")
	if err != nil || len(es) != 1 || es[0].Name != "a.txt" {
		t.Fatalf("%+v %v", es, err)
	}
	if err := h.app.Sftp.Close(info.SID); err != nil || !fs.closed.Load() {
		t.Fatalf("the session closes the connection: %v", err)
	}
}

func TestOpenFTPFallsBackToTheUserNameOfTheHostAndToAnonymous(t *testing.T) {
	h := newHarness(t).unlocked(t)
	var seen FTPConfig
	h.core.OpenFTP = func(cfg FTPConfig) (RemoteFS, error) { seen = cfg; return &fakeFTPFS{}, nil }
	host := ftpHost(t, h, nil)
	if _, err := h.app.Sftp.Open(FTPID(host.ID), "k"); err != nil || seen.User != "web" || seen.Password != "" {
		t.Fatalf("%+v %v", seen, err)
	}
	anon := ftpHost(t, h, func(x *domain.Host) { x.Label = "anon"; x.Username = "" })
	if _, err := h.app.Sftp.Open(FTPID(anon.ID), "k"); err != nil || seen.User != "" {
		t.Fatalf("an empty user is anonymous for the adapter: %+v %v", seen, err)
	}
}

func TestOpenFTPRefusesWhatCannotWork(t *testing.T) {
	h := newHarness(t).unlocked(t)
	h.core.OpenFTP = func(FTPConfig) (RemoteFS, error) { return &fakeFTPFS{}, nil }
	off := ftpHost(t, h, func(x *domain.Host) { x.FTPEnabled = false })
	if _, err := h.app.Sftp.Open(FTPID(off.ID), "k"); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("%v", err)
	}
	jump := ftpHost(t, h, func(x *domain.Host) { x.Label = "behind"; x.JumpHostID = off.ID })
	if _, err := h.app.Sftp.Open(FTPID(jump.ID), "k"); err == nil || !strings.Contains(err.Error(), "jump host") {
		t.Fatalf("%v", err)
	}
	if _, err := h.app.Sftp.Open(FTPID("nope"), "k"); err == nil {
		t.Fatal("a host that does not exist")
	}
	h.core.OpenFTP = nil
	on := ftpHost(t, h, func(x *domain.Host) { x.Label = "x2" })
	if _, err := h.app.Sftp.Open(FTPID(on.ID), "k"); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("%v", err)
	}
}

func TestFTPSessionEndsWhenThePingFails(t *testing.T) {
	h := newHarness(t).unlocked(t)
	fs := &fakeFTPFS{failAt: 2}
	h.core.OpenFTP = func(FTPConfig) (RemoteFS, error) { return fs, nil }
	host := ftpHost(t, h, func(x *domain.Host) { x.KeepaliveSec = 1 })
	info, err := h.app.Sftp.Open(FTPID(host.ID), "k")
	if err != nil {
		t.Fatal(err)
	}
	h.rec.wait(t, "sftp:closed:"+info.SID)
	if !fs.closed.Load() {
		t.Fatal("the connection was not closed")
	}
	if _, err := h.app.Sftp.List(info.SID, "/"); err == nil {
		t.Fatal("the session is gone")
	}
	_ = time.Second
}

func TestDeletingAHostForgetsItsFTPPassword(t *testing.T) {
	h := newHarness(t).unlocked(t)
	host := ftpHost(t, h, nil)
	if err := h.app.Host.SetFTPPassword(host.ID, "pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.core.Vault.GetSecret(domain.FTPSecretRef(host.ID)); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Host.DeleteHost(host.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.core.Vault.GetSecret(domain.FTPSecretRef(host.ID)); err == nil {
		t.Fatal("the password is still in the vault")
	}
	if err := h.app.Host.SetFTPPassword("nope", "x"); err == nil {
		t.Fatal("a password for a host that does not exist")
	}
}
