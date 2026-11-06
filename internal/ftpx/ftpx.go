// Package ftpx reads and writes files on an FTP or FTPS server, for the SFTP tab: the same operations as sftpx, over another protocol.
// Plain FTP sends the password and the files in the clear; FTPS (explicit or implicit TLS) encrypts them and checks the certificate.
package ftpx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/jlaffaye/ftp"

	"vutty/internal/domain"
)

// Config says how to reach the server.
type Config struct {
	Addr     string // host:port
	Security string // "" (plain), "explicit" (FTPS: AUTH TLS) or "implicit" (TLS from the first byte)
	User     string
	Password string
	// ServerName is the name the certificate must be for (empty: the host of Addr).
	ServerName string
	Timeout    time.Duration
	// TLS lets a test trust its own certificate; nil uses the system roots.
	TLS *tls.Config
}

// FS is a connection to an FTP server. A FTP control connection serves one command at a time, so every call is serialised.
type FS struct {
	mu   sync.Mutex
	cfg  Config
	conn *ftp.ServerConn
	home string
}

// Open connects and logs in. An empty user logs in as "anonymous".
func Open(cfg Config) (*FS, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	f := &FS{cfg: cfg}
	if err := f.dial(); err != nil {
		return nil, err
	}
	home, err := f.conn.CurrentDir()
	if err != nil {
		_ = f.conn.Quit()
		return nil, fmt.Errorf("read the current folder: %w", err)
	}
	f.home = home
	return f, nil
}

func (f *FS) tlsConfig() *tls.Config {
	if f.cfg.TLS != nil {
		return f.cfg.TLS
	}
	name := f.cfg.ServerName
	if name == "" {
		name, _, _ = net.SplitHostPort(f.cfg.Addr)
	}
	return &tls.Config{ServerName: name, MinVersion: tls.VersionTLS12}
}

func (f *FS) dial() error {
	opts := []ftp.DialOption{ftp.DialWithTimeout(f.cfg.Timeout)}
	switch f.cfg.Security {
	case "":
	case "explicit":
		opts = append(opts, ftp.DialWithExplicitTLS(f.tlsConfig()))
	case "implicit":
		opts = append(opts, ftp.DialWithTLS(f.tlsConfig()))
	default:
		return fmt.Errorf("unknown FTP security %q", f.cfg.Security)
	}
	c, err := ftp.Dial(f.cfg.Addr, opts...)
	if err != nil {
		return friendly(err)
	}
	user, pass := f.cfg.User, f.cfg.Password
	if user == "" {
		user, pass = "anonymous", "anonymous@"
	}
	if err := c.Login(user, pass); err != nil {
		_ = c.Quit()
		return friendly(err)
	}
	f.conn = c
	return nil
}

func friendly(err error) error {
	var x *tls.CertificateVerificationError
	if errors.As(err, &x) {
		return fmt.Errorf("the certificate of the server cannot be trusted: %w", err)
	}
	return err
}

func (f *FS) Home() (string, error) { return f.home, nil }

func (f *FS) List(dir string) ([]domain.RemoteEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if dir == "" {
		dir = f.home
	}
	es, err := f.conn.List(dir)
	if err != nil {
		return nil, err
	}
	out := make([]domain.RemoteEntry, 0, len(es))
	for _, e := range es {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		out = append(out, entryOf(dir, e))
	}
	return out, nil
}

func entryOf(dir string, e *ftp.Entry) domain.RemoteEntry {
	r := domain.RemoteEntry{Name: e.Name, Path: path.Join(dir, e.Name), Size: int64(e.Size), ModTime: e.Time}
	switch e.Type {
	case ftp.EntryTypeFolder:
		r.IsDir = true
	case ftp.EntryTypeLink:
		r.IsLink = true
	}
	r.Mode = "-rw-r--r--"
	if r.IsDir {
		r.Mode = "drwxr-xr-x"
	}
	return r
}

func (f *FS) Mkdir(p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conn.MakeDir(p)
}

func (f *FS) Rename(from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conn.Rename(from, to)
}

// Chmod is not offered: the FTP library has no way to send SITE CHMOD, and a server may not know it.
func (f *FS) Chmod(string, uint32) error {
	return errors.New("permissions cannot be changed over FTP")
}

func (f *FS) Remove(p string, recursive bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if recursive {
		return f.conn.RemoveDirRecur(p)
	}
	if err := f.conn.Delete(p); err == nil {
		return nil
	}
	return f.conn.RemoveDir(p)
}

func (f *FS) Stat(p string) (domain.RemoteEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dir, name := path.Split(strings.TrimRight(p, "/"))
	if dir == "" {
		dir = "."
	}
	es, err := f.conn.List(dir)
	if err != nil {
		return domain.RemoteEntry{}, err
	}
	for _, e := range es {
		if e.Name == name {
			return entryOf(strings.TrimRight(dir, "/"), e), nil
		}
	}
	return domain.RemoteEntry{}, os.ErrNotExist
}

// Upload copies a local file to the server, from where a partial one ends when the server has it already (REST).
func (f *FS) Upload(ctx context.Context, local, remote string, overwrite bool, progress func(done, total int64)) error {
	fi, err := os.Stat(local)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var offset uint64
	if es, err := f.conn.List(path.Dir(remote)); err == nil {
		for _, e := range es {
			if e.Name == path.Base(remote) {
				if !overwrite && int64(e.Size) >= fi.Size() {
					return os.ErrExist
				}
				if !overwrite && int64(e.Size) < fi.Size() {
					offset = e.Size
				}
			}
		}
	}
	src, err := os.Open(local)
	if err != nil {
		return err
	}
	defer src.Close()
	if offset > 0 {
		if _, err := src.Seek(int64(offset), io.SeekStart); err != nil {
			return err
		}
	}
	r := &progressReader{ctx: ctx, r: src, done: int64(offset), total: fi.Size(), progress: progress}
	if offset > 0 {
		return f.conn.StorFrom(remote, r, offset)
	}
	return f.conn.Stor(remote, r)
}

// Download copies a file from the server to a local one, continuing a partial local file.
func (f *FS) Download(ctx context.Context, remote, local string, overwrite bool, progress func(done, total int64)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	size, err := f.conn.FileSize(remote)
	if err != nil {
		size = 0
	}
	var offset int64
	flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if st, err := os.Stat(local); err == nil {
		switch {
		case overwrite:
		case st.Size() > 0 && size > 0 && st.Size() < size:
			offset, flag = st.Size(), os.O_WRONLY|os.O_APPEND
		default:
			return os.ErrExist
		}
	}
	var resp *ftp.Response
	if offset > 0 {
		resp, err = f.conn.RetrFrom(remote, uint64(offset))
	} else {
		resp, err = f.conn.Retr(remote)
	}
	if err != nil {
		return err
	}
	defer resp.Close()
	dst, err := os.OpenFile(local, flag, 0o644)
	if err != nil {
		return err
	}
	defer dst.Close()
	w := &progressWriter{ctx: ctx, w: dst, done: offset, total: size, progress: progress}
	if _, err := io.Copy(w, resp); err != nil {
		return err
	}
	return nil
}

// Ping keeps the control connection alive.
func (f *FS) Ping() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conn.NoOp()
}

func (f *FS) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.conn == nil {
		return nil
	}
	err := f.conn.Quit()
	f.conn = nil
	return err
}

type progressReader struct {
	ctx         context.Context
	r           io.Reader
	done, total int64
	progress    func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	if err := context.Cause(p.ctx); err != nil {
		return 0, err
	}
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.progress != nil && n > 0 {
		p.progress(p.done, p.total)
	}
	return n, err
}

type progressWriter struct {
	ctx         context.Context
	w           io.Writer
	done, total int64
	progress    func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	if err := context.Cause(p.ctx); err != nil {
		return 0, err
	}
	n, err := p.w.Write(b)
	p.done += int64(n)
	if p.progress != nil && n > 0 {
		p.progress(p.done, p.total)
	}
	return n, err
}
