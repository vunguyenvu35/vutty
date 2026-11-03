package usecase

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"

	"vutty/internal/domain"
)

const (
	execMaxHosts    = 200
	execConcurrency = 5
	execOutputCap   = 64 << 10 // kept per host: the head of what the command printed
	execMaxCommand  = 16 << 10
	execKeepRuns    = 5
	execDefaultSecs = 60
	execMaxSecs     = 600
)

// ExecSpec is one command to run on several hosts.
type ExecSpec struct {
	HostIDs []string `json:"host_ids"`
	// Command is run as it is, without a terminal. {{host}}, {{hostname}} and {{user}} become the label, the address and
	// the user name of each host.
	Command    string `json:"command"`
	TimeoutSec int    `json:"timeout_sec"` // per host; default 60
}

// ExecResult is how the command went on one host.
type ExecResult struct {
	HostID     string `json:"host_id"`
	Label      string `json:"label"`
	Env        string `json:"env,omitempty"`
	State      string `json:"state"` // queued, running, ok, failed, canceled
	ExitCode   int    `json:"exit_code"`
	Output     string `json:"output"` // standard output and standard error together
	Truncated  bool   `json:"truncated,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// ExecRun is the state of a run, sent as a whole with every change.
type ExecRun struct {
	ID      string       `json:"id"`
	Done    bool         `json:"done"`
	Results []ExecResult `json:"results"`
}

type execRun struct {
	mu    sync.Mutex
	run   ExecRun
	abort chan struct{}
	once  sync.Once
}

// ExecService runs one command on many hosts at once, each on its own connection, and keeps what came back. It asks nothing
// while it runs (see Core.connectQuiet), so every host has to be one you can already reach without a question.
type ExecService struct {
	c *Core

	mu    sync.Mutex
	runs  map[string]*execRun
	order []string
}

func NewExecService(c *Core) *ExecService { return &ExecService{c: c, runs: map[string]*execRun{}} }

// Run starts the command on every host and returns the id of the run; progress arrives as exec:update:<id> events that carry the
// whole ExecRun. At most five hosts run at a time.
func (s *ExecService) Run(spec ExecSpec) (string, error) {
	if !s.c.Vault.Status().Unlocked {
		return "", errNotUnlocked
	}
	cmd := strings.TrimSpace(spec.Command)
	switch {
	case cmd == "":
		return "", errors.New("there is no command to run")
	case len(spec.Command) > execMaxCommand:
		return "", fmt.Errorf("the command is longer than %d KB", execMaxCommand>>10)
	case strings.ContainsRune(spec.Command, 0):
		return "", errors.New("the command contains a NUL character")
	case len(spec.HostIDs) == 0:
		return "", errors.New("choose at least one host")
	case len(spec.HostIDs) > execMaxHosts:
		return "", fmt.Errorf("at most %d hosts at a time", execMaxHosts)
	}
	secs := spec.TimeoutSec
	if secs <= 0 {
		secs = execDefaultSecs
	}
	if secs > execMaxSecs {
		secs = execMaxSecs
	}
	byID := map[string]domain.Host{}
	for _, h := range s.c.Store.ListHosts(domain.HostFilter{}) {
		byID[h.ID] = h
	}
	run := &execRun{abort: make(chan struct{}), run: ExecRun{ID: domain.NewID()}}
	seen := map[string]bool{}
	for _, id := range spec.HostIDs {
		h, ok := byID[id]
		if !ok {
			return "", errors.New("a chosen host no longer exists")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		run.run.Results = append(run.run.Results, ExecResult{HostID: id, Label: h.Label, Env: string(h.Env), State: "queued"})
	}

	s.mu.Lock()
	s.runs[run.run.ID] = run
	s.order = append(s.order, run.run.ID)
	for len(s.order) > execKeepRuns { // the oldest runs, with their output, are forgotten
		delete(s.runs, s.order[0])
		s.order = s.order[1:]
	}
	s.mu.Unlock()

	go s.work(run, spec.Command, time.Duration(secs)*time.Second)
	return run.run.ID, nil
}

func (s *ExecService) work(r *execRun, command string, timeout time.Duration) {
	s.push(r)
	sem := make(chan struct{}, execConcurrency)
	var wg sync.WaitGroup
	for i := range r.run.Results {
		select {
		case sem <- struct{}{}:
		case <-r.abort:
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() {
				select {
				case <-sem:
				default:
				}
			}()
			s.runOne(r, i, command, timeout)
		}(i)
	}
	wg.Wait()
	r.mu.Lock()
	r.run.Done = true
	r.mu.Unlock()
	s.push(r)
}

// push sends the state of a run to the screen.
func (s *ExecService) push(r *execRun) { s.c.emit("exec:update:"+r.run.ID, snapshot(r)) }

func snapshot(r *execRun) ExecRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.run
	out.Results = append([]ExecResult(nil), r.run.Results...)
	return out
}

func (s *ExecService) set(r *execRun, i int, f func(*ExecResult)) {
	r.mu.Lock()
	f(&r.run.Results[i])
	r.mu.Unlock()
	s.push(r)
}

func (s *ExecService) runOne(r *execRun, i int, command string, timeout time.Duration) {
	select {
	case <-r.abort:
		s.set(r, i, func(x *ExecResult) { x.State = "canceled" })
		return
	default:
	}
	r.mu.Lock()
	id, label := r.run.Results[i].HostID, r.run.Results[i].Label
	r.mu.Unlock()
	s.set(r, i, func(x *ExecResult) { x.State = "running" })
	start := time.Now()
	finish := func(state string, code int, out *capped, errMsg string) {
		ok := state == "ok"
		msg := "Ran a command (exit " + fmt.Sprint(code) + ")"
		if errMsg != "" {
			msg = "Could not run a command: " + errMsg
		}
		if state == "canceled" {
			msg = "A command was cancelled"
		}
		s.c.record(domain.ActConnect, id, label, msg, ok) // never the command or its output: they may hold secrets
		s.set(r, i, func(x *ExecResult) {
			x.State, x.ExitCode, x.Error, x.DurationMS = state, code, errMsg, time.Since(start).Milliseconds()
			if out != nil {
				x.Output, x.Truncated = out.String(), out.truncated
			}
		})
	}

	cl, host, err := s.c.connectQuiet(id)
	if err != nil {
		finish("failed", -1, nil, err.Error())
		return
	}
	defer cl.Close()
	sess, err := cl.Client.NewSession()
	if err != nil {
		finish("failed", -1, nil, err.Error())
		return
	}
	defer sess.Close()
	out := &capped{max: execOutputCap}
	sess.Stdout, sess.Stderr = out, out

	var timedOut, canceled atomic.Bool
	finished := make(chan struct{})
	defer close(finished)
	timer := time.AfterFunc(timeout, func() { timedOut.Store(true); cl.Close() })
	defer timer.Stop()
	go func() {
		select {
		case <-r.abort:
			canceled.Store(true)
			cl.Close()
		case <-finished:
		}
	}()

	err = sess.Run(expandHostVars(command, host))
	switch {
	case canceled.Load():
		finish("canceled", -1, out, "")
	case timedOut.Load():
		finish("failed", -1, out, fmt.Sprintf("timed out after %d seconds", int(timeout.Seconds())))
	case err == nil:
		finish("ok", 0, out, "")
	default:
		var ee *ssh.ExitError
		if errors.As(err, &ee) {
			finish("failed", ee.ExitStatus(), out, fmt.Sprintf("exited with status %d", ee.ExitStatus()))
		} else {
			finish("failed", -1, out, err.Error())
		}
	}
}

// Cancel stops a run: hosts that are running are disconnected, hosts that have not started never do.
func (s *ExecService) Cancel(id string) error {
	r, err := s.get(id)
	if err != nil {
		return err
	}
	r.once.Do(func() { close(r.abort) })
	return nil
}

// Get returns the state of a run (the same thing the exec:update events carry).
func (s *ExecService) Get(id string) (ExecRun, error) {
	r, err := s.get(id)
	if err != nil {
		return ExecRun{}, err
	}
	return snapshot(r), nil
}

// Forget drops a run and what it printed. A run that is still going is cancelled first.
func (s *ExecService) Forget(id string) error {
	r, err := s.get(id)
	if err != nil {
		return err
	}
	r.once.Do(func() { close(r.abort) })
	s.mu.Lock()
	delete(s.runs, id)
	for i, x := range s.order {
		if x == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *ExecService) get(id string) (*execRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, errors.New("that run is no longer kept")
	}
	return r, nil
}

// capped keeps the first max bytes written to it (and says whether more came), safe for the two copies of one session.
type capped struct {
	mu        sync.Mutex
	buf       []byte
	max       int
	truncated bool
}

func (c *capped) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := c.max - len(c.buf); room > 0 {
		take := p
		if len(take) > room {
			take = take[:room]
			c.truncated = true
		}
		c.buf = append(c.buf, take...)
	} else if len(p) > 0 {
		c.truncated = true
	}
	return len(p), nil
}

func (c *capped) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.ToValidUTF8(string(c.buf), string(utf8.RuneError))
}

var hostVars = regexp.MustCompile(`\{\{\s*(host|hostname|user)\s*\}\}`)
var plainValue = regexp.MustCompile(`^[A-Za-z0-9._@:/+=,-]+$`)

// expandHostVars puts the label, the address and the user name of a host where the command has {{host}}, {{hostname}} and
// {{user}}. A value with anything a shell would read is put in single quotes, so a strange label cannot add a command.
func expandHostVars(command string, h domain.Host) string {
	return hostVars.ReplaceAllStringFunc(command, func(m string) string {
		var v string
		switch strings.Trim(m, "{} \t") {
		case "host":
			v = h.Label
		case "hostname":
			v = h.Hostname
		default:
			v = h.Username
		}
		if plainValue.MatchString(v) {
			return v
		}
		return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
	})
}
