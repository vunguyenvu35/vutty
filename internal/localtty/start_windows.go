//go:build windows

package localtty

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

// winProc is a process on a ConPTY (Windows 10 1809 and later).
type winProc struct {
	c      *conpty.ConPty
	once   sync.Once
	closed atomic.Bool
}

func startProc(cmd *exec.Cmd, cols, rows int) (proc, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, errors.New("the local terminal needs Windows 10 version 1809 or later")
	}
	opts := []conpty.ConPtyOption{conpty.ConPtyDimensions(cols, rows), conpty.ConPtyEnv(cmd.Env)}
	if cmd.Dir != "" {
		opts = append(opts, conpty.ConPtyWorkDir(cmd.Dir))
	}
	args := cmd.Args
	if len(args) == 0 {
		args = []string{cmd.Path}
	}
	c, err := conpty.Start(windows.ComposeCommandLine(args), opts...)
	if err != nil {
		return nil, err
	}
	return &winProc{c: c}, nil
}

func (p *winProc) Read(b []byte) (int, error)  { return p.c.Read(b) }
func (p *winProc) Write(b []byte) (int, error) { return p.c.Write(b) }
func (p *winProc) resize(cols, rows int) error { return p.c.Resize(cols, rows) }
func (p *winProc) pid() int                    { return p.c.Pid() }
func (p *winProc) Close() (err error) {
	p.once.Do(func() { p.closed.Store(true); err = p.c.Close() }) // the library's Close is not safe to repeat
	return err
}
func (p *winProc) wait() error {
	// The exit code is not an error: the shell simply ended. After Close the handle is gone, which is
	// also a normal end (the user closed the tab).
	_, err := p.c.Wait(context.Background())
	if p.closed.Load() {
		return nil
	}
	return err
}

// hangup ends the shell; Close then releases the pseudo-console, which unblocks the reader.
func (p *winProc) hangup() {
	if proc, err := os.FindProcess(p.c.Pid()); err == nil {
		_ = proc.Kill()
	}
}

// UserShell is PowerShell 7 when installed, else Windows PowerShell, else %ComSpec% (cmd.exe).
func UserShell() string {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if s := strings.TrimSpace(os.Getenv("ComSpec")); s != "" {
		return s
	}
	return "cmd.exe"
}

func loginArgs() []string { return nil }
