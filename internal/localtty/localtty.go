// Package localtty runs a shell on this computer behind a pseudo-terminal: the "Terminal" tab, like
// the Terminal app but inside Vutty. It implements the usecase.Shell port. Unix uses a PTY, Windows
// uses ConPTY.
package localtty

import (
	"errors"
	"os"
	"os/exec"
	"sync"
)

// proc is a process attached to a pseudo-terminal, whichever way the platform does that.
type proc interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	resize(cols, rows int) error
	wait() error // blocks until the process ended; a non-zero exit status is not an error
	pid() int
	hangup() // asks the process (group) to stop
	Close() error
}

// Shell is a running local process on a PTY.
type Shell struct {
	p    proc
	done chan struct{}
	err  error
	once sync.Once
}

// Open starts the user's login shell in their home folder. onData receives the terminal output and must
// not block for long.
func Open(cols, rows int, onData func([]byte)) (*Shell, error) {
	cmd := exec.Command(UserShell(), loginArgs()...)
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	return OpenCommand(cmd, cols, rows, onData)
}

// OpenCommand runs any command on a PTY (used by tests with a tiny script).
func OpenCommand(cmd *exec.Cmd, cols, rows int, onData func([]byte)) (*Shell, error) {
	if cols < 1 || rows < 1 {
		cols, rows = 80, 24
	}
	cmd.Env = append(append(os.Environ(), cmd.Env...), "TERM=xterm-256color", "COLORTERM=truecolor") // what the caller put in cmd.Env (a key, say) is kept
	p, err := startProc(cmd, cols, rows)
	if err != nil {
		return nil, err
	}
	s := &Shell{p: p, done: make(chan struct{})}
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				onData(append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				break // EIO when the process ended
			}
		}
		werr := p.wait()
		var ee *exec.ExitError
		if werr != nil && !errors.As(werr, &ee) {
			s.err = werr
		}
		p.Close()
		close(s.done)
	}()
	return s, nil
}

func (s *Shell) Write(p []byte) (int, error) { return s.p.Write(p) }

func (s *Shell) Resize(cols, rows int) error { return s.p.resize(cols, rows) }

// Done is closed when the process has ended.
func (s *Shell) Done() <-chan struct{} { return s.done }

// Err is nil for a normal end (including a non-zero exit status: the shell simply exited).
func (s *Shell) Err() error { <-s.done; return s.err }

// Close hangs up the terminal and stops the process group.
func (s *Shell) Close() {
	s.once.Do(func() {
		s.p.hangup()
		s.p.Close()
	})
}

// Cwd is the folder the shell is in now ("" when it cannot be told).
func (s *Shell) Cwd() string {
	if pid := s.p.pid(); pid > 0 {
		return processCwd(pid)
	}
	return ""
}
