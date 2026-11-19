//go:build !windows

package localtty

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type unixProc struct {
	cmd *exec.Cmd
	tty *os.File
}

func startProc(cmd *exec.Cmd, cols, rows int) (proc, error) {
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &unixProc{cmd: cmd, tty: tty}, nil
}

func (p *unixProc) Read(b []byte) (int, error)  { return p.tty.Read(b) }
func (p *unixProc) Write(b []byte) (int, error) { return p.tty.Write(b) }
func (p *unixProc) Close() error                { return p.tty.Close() }
func (p *unixProc) wait() error                 { return p.cmd.Wait() }
func (p *unixProc) resize(cols, rows int) error {
	return pty.Setsize(p.tty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
func (p *unixProc) pid() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
func (p *unixProc) hangup() {
	if p.cmd.Process != nil {
		hangup(p.cmd.Process)
	}
}

// UserShell is what the user's account runs: $SHELL, else /bin/zsh on macOS and /bin/sh elsewhere.
func UserShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if _, err := os.Stat("/bin/zsh"); err == nil {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

func loginArgs() []string { return []string{"-l"} }
