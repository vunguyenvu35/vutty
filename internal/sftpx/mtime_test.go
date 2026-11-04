package sftpx

import (
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"vutty/internal/sshx"
	"vutty/internal/testssh"
)

// newRealFS is a session on the real file system of the test process: the in-memory server of newFS ignores the times
// a client sets, and these tests are about times.
func newRealFS(t *testing.T) *FS {
	t.Helper()
	srv, err := testssh.Start(testssh.Config{User: "u", Password: "p", SFTP: true, SFTPRealFS: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	d := &sshx.Dialer{HostKeys: &sshx.HostKeyChecker{Path: filepath.Join(t.TempDir(), "kh"),
		Prompt: func(string, ssh.PublicKey) (bool, error) { return true, nil }}, Timeout: 5 * time.Second}
	cl, err := d.Dial([]sshx.Hop{{Label: "t", Hostname: "127.0.0.1", Port: srv.Port(), Username: "u", Auth: sshx.PasswordAuth("p")}})
	if err != nil {
		t.Fatal(err)
	}
	fs, err := Open(cl.Client)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close(); cl.Close() })
	return fs
}

// write creates a file with the given modification time.
func write(t *testing.T, p, content string, mod time.Time) string {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUploadKeepsTheTimeOfTheSource(t *testing.T) {
	fs := newRealFS(t)
	dir := t.TempDir() // the "server" side
	mod := time.Now().Add(-90 * 24 * time.Hour)
	src := write(t, filepath.Join(t.TempDir(), "a.txt"), "data", mod)
	if err := fs.Upload(bg, src, path.Join(dir, "a.txt"), false, nil); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if st.ModTime().Unix() != mod.Unix() {
		t.Fatalf("new file: remote time %v, source %v", st.ModTime(), mod)
	}
	later := mod.Add(24 * time.Hour)
	write(t, src, "changed", later)
	if err := fs.Upload(bg, src, path.Join(dir, "a.txt"), true, nil); err != nil {
		t.Fatal(err)
	}
	if st, _ = os.Stat(filepath.Join(dir, "a.txt")); st.ModTime().Unix() != later.Unix() {
		t.Fatalf("replaced file: remote time %v, source %v", st.ModTime(), later)
	}
}

func TestDownloadKeepsTheTimeOfTheSource(t *testing.T) {
	fs := newRealFS(t)
	mod := time.Now().Add(-45 * 24 * time.Hour)
	remote := write(t, filepath.Join(t.TempDir(), "b.txt"), "data", mod)
	dst := filepath.Join(t.TempDir(), "b.txt")
	if err := fs.Download(bg, remote, dst, false, nil); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if st.ModTime().Unix() != mod.Unix() {
		t.Fatalf("local time %v, remote %v", st.ModTime(), mod)
	}
}

func TestUploadSweepsOnlyAbandonedPartialCopies(t *testing.T) {
	fs := newRealFS(t)
	dir := t.TempDir()
	old := time.Now().Add(-(partMaxAge + time.Hour))
	write(t, filepath.Join(dir, "old.bin.vutty-part-0123abcd"), "stale", old)
	write(t, filepath.Join(dir, "fresh.bin.vutty-part-0123abcd"), "recent", time.Now())
	write(t, filepath.Join(dir, "note.vutty-part"), "not a partial copy: no fingerprint", old)
	write(t, filepath.Join(dir, "plain.txt"), "plain", old)
	src := write(t, filepath.Join(t.TempDir(), "n.txt"), "x", time.Now())
	if err := fs.Upload(bg, src, path.Join(dir, "n.txt"), false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.bin.vutty-part-0123abcd")); err == nil {
		t.Fatal("an abandoned partial copy was kept")
	}
	for _, keep := range []string{"fresh.bin.vutty-part-0123abcd", "note.vutty-part", "plain.txt", "n.txt"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Fatalf("%s was deleted: %v", keep, err)
		}
	}
}

func TestDownloadSweepsTheTargetFolderOncePerSession(t *testing.T) {
	fs := newRealFS(t)
	remote := t.TempDir()
	write(t, filepath.Join(remote, "a.txt"), "a", time.Now())
	write(t, filepath.Join(remote, "b.txt"), "b", time.Now())
	dir := t.TempDir()
	old := time.Now().Add(-(partMaxAge + time.Hour))
	first := write(t, filepath.Join(dir, ".x.txt.vutty-part-0123abcd"), "x", old)
	fresh := write(t, filepath.Join(dir, ".y.txt.vutty-part-0123abcd"), "recent", time.Now())
	if err := fs.Download(bg, path.Join(remote, "a.txt"), filepath.Join(dir, "a.txt"), false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first); err == nil {
		t.Fatal("an abandoned partial download was kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a recent partial download was deleted")
	}
	second := write(t, filepath.Join(dir, ".z.txt.vutty-part-0123abcd"), "z", old)
	if err := fs.Download(bg, path.Join(remote, "b.txt"), filepath.Join(dir, "b.txt"), false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatal("the folder was swept again for every file")
	}
}
