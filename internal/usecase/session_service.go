package usecase

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"vutty/internal/domain"
	"vutty/internal/sshx"
)

const (
	outputBatchInterval = 16 * time.Millisecond
	outputBatchMax      = 64 * 1024
)

type session struct {
	sid    string
	hostID string

	mu     sync.Mutex
	cols   int
	rows   int
	client *sshx.Client
	shell  Shell
	closed bool
	abort  chan struct{} // closed on Close(sid) to cancel connecting
	stopKA chan struct{}

	// Until the frontend calls Attach, output is buffered and state events can be replayed,
	// so nothing emitted between Open returning and the listeners existing is lost.
	attached bool
	pend     []byte
	// screen is the tail of the raw terminal output (what the AI assistant may read) and screenSeq counts every
	// byte ever appended, so a reader can ask for "what came after".
	screen    []byte
	screenSeq int64
	state     string // connecting | ready | closed
	closedEv  any
	ready     any
	log       *sessionLog // the file this connection's output goes to (nil when logging is off)
	gate      *sshx.Gate  // flow control: output waits while the terminal is behind
}

const maxPending = 1 << 20

// flowWindow is how much output may be on its way to the terminal, sent but not yet parsed, before reading the connection
// pauses (the terminal reports through Ack). Large enough to keep a fast link busy, small enough that a terminal that is
// slow to draw never holds more than a second or so of output.
const flowWindow = 512 << 10

// maxScreen bounds the output kept for the assistant.
const maxScreen = 256 << 10

// appendScreen keeps the tail of the output; the caller holds sess.mu.
func (x *session) appendScreen(p []byte) {
	x.screen = append(x.screen, p...)
	x.screenSeq += int64(len(p))
	if len(x.screen) > maxScreen {
		x.screen = append([]byte(nil), x.screen[len(x.screen)-maxScreen:]...)
	}
}

// SessionService owns terminal tabs. Output flows to the frontend via events and keystrokes
// come back through Write; the frontend never holds a socket or a secret.
type SessionService struct {
	c *Core
	f *ForwardService

	mu       sync.Mutex
	sessions map[string]*session
	// OnClose is told when a tab is closed (the assistant forgets its conversation).
	OnClose func(sid string)
}

func NewSessionService(c *Core, f *ForwardService) *SessionService {
	s := &SessionService{c: c, f: f, sessions: map[string]*session{}}
	f.sessions = s
	return s
}

// Open starts connecting to a host and returns the session id immediately. Progress is
// reported through term:hostkey:<sid>, term:ready:<sid>, term:data:<sid>, term:closed:<sid>.
func (s *SessionService) Open(hostID string, cols, rows int) (string, error) {
	switch {
	case hostID == LocalHostID:
	case strings.HasPrefix(hostID, serialPrefix):
		if _, _, err := ParseSerialID(hostID); err != nil {
			return "", err
		}
	case strings.HasPrefix(hostID, kubeExecPrefix):
		if _, ok := s.c.kubeShell(hostID); !ok {
			return "", errors.New("this shell is no longer known: open it again from the pod")
		}
	case strings.HasPrefix(hostID, moshPrefix):
		if _, err := s.c.Store.GetHost(strings.TrimPrefix(hostID, moshPrefix)); err != nil {
			return "", fmt.Errorf("host: %w", err)
		}
	case strings.HasPrefix(hostID, telnetPrefix):
		h, err := s.c.Store.GetHost(strings.TrimPrefix(hostID, telnetPrefix))
		if err != nil {
			return "", fmt.Errorf("host: %w", err)
		}
		if !h.TelnetEnabled {
			return "", fmt.Errorf("Telnet is not enabled for %q: edit the host and turn it on", h.Label)
		}
	default:
		if _, err := s.c.Store.GetHost(hostID); err != nil {
			return "", fmt.Errorf("host: %w", err)
		}
	}
	if cols < 1 || rows < 1 {
		cols, rows = 80, 24
	}
	sess := &session{sid: domain.NewID(), hostID: hostID, cols: cols, rows: rows, abort: make(chan struct{}), gate: sshx.NewGate(flowWindow)}
	s.mu.Lock()
	s.sessions[sess.sid] = sess
	s.mu.Unlock()
	go s.run(sess)
	return sess.sid, nil
}

func (s *SessionService) get(sid string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x, ok := s.sessions[sid]; ok {
		return x, nil
	}
	return nil, errors.New("unknown session")
}

func (s *SessionService) run(sess *session) {
	if isDeviceID(sess.hostID) {
		s.runDevice(sess)
		return
	}
	sid := sess.sid
	closed := func(reason string, clean bool) {
		s.logEnd(sess, reason, clean)
		sess.mu.Lock()
		defer sess.mu.Unlock()
		sess.state = "closed"
		sess.closedEv = map[string]any{"reason": reason, "clean": clean}
		s.c.emit("term:closed:"+sid, sess.closedEv)
	}
	sess.mu.Lock()
	sess.state, sess.closedEv, sess.ready = "connecting", nil, nil
	sess.mu.Unlock()
	cl, host, err := s.c.connect(sess.hostID, sid, sess.abort)
	if err != nil {
		closed(err.Error(), false)
		return
	}
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		cl.Close()
		return
	}
	cols, rows := sess.cols, sess.rows
	sess.mu.Unlock()

	if host.ForwardAgent {
		s.c.enableAgentForwarding(cl, host)
	}
	batch := s.batcher(sess)
	sh, err := cl.OpenShell(cols, rows, batch.Write)
	if err != nil {
		cl.Close()
		closed(err.Error(), false)
		return
	}
	stop := make(chan struct{})
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		sh.Close()
		return
	}
	sess.client, sess.shell, sess.stopKA = cl, sh, stop
	sess.mu.Unlock()

	go sshx.Keepalive(cl, time.Duration(host.KeepaliveSec)*time.Second, 3, stop, nil)
	// the terminal is ready as soon as the shell is open: what follows (the log line, the server type, the
	// forwards that start with the terminal) must not keep the connecting screen waiting
	sess.mu.Lock()
	sess.state, sess.ready = "ready", map[string]any{"host": host.Label}
	s.c.emit("term:ready:"+sid, sess.ready)
	sess.mu.Unlock()
	s.c.record(domain.ActConnect, host.ID, host.Label, fmt.Sprintf("Connected to %s:%d", host.Hostname, host.Port), true)
	s.c.learnOS(host, cl)
	s.f.autoStart(sid, cl, host.ID)

	<-sh.Done()
	err = sh.Err()
	close(stop)
	batch.Close()
	s.f.stopOwned(sid)
	sh.Close()

	sess.mu.Lock()
	wasClosed := sess.closed
	sess.client, sess.shell = nil, nil
	sess.mu.Unlock()
	if wasClosed {
		s.c.record(domain.ActDisconnect, sess.hostID, "", "Closed by you", true)
		return // user closed the tab: no event needed
	}
	if err == nil {
		closed("session ended", true)
		return
	}
	closed("connection lost: "+err.Error(), false)
}

// Attach tells the backend the frontend is now listening for this session's events. Output
// buffered so far is delivered and the current state (pending host key prompt, ready, closed)
// is replayed.
func (s *SessionService) Attach(sid string) error {
	sess, err := s.get(sid)
	if err != nil {
		return err
	}
	if p, ok := s.c.prompts.payload(sid); ok {
		s.c.emit("term:hostkey:"+sid, p)
	}
	if p, ok := s.c.authPrompts.payload(sid); ok {
		s.c.emit("term:auth:"+sid, p)
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	switch sess.state {
	case "ready":
		s.c.emit("term:ready:"+sid, sess.ready)
	case "closed":
		s.c.emit("term:closed:"+sid, sess.closedEv)
	}
	sess.gate.Reset() // this terminal counts what it receives from here on
	if len(sess.pend) > 0 {
		sess.gate.Sent(len(sess.pend))
		s.c.emit("term:data:"+sid, base64.StdEncoding.EncodeToString(sess.pend))
		sess.pend = nil
	}
	sess.attached = true
	return nil
}

// touchVault tells the vault that a terminal is in use, so the auto-lock timer measures real inactivity.
func (s *SessionService) touchVault() {
	if s.c.Vault != nil {
		s.c.Vault.Touch()
	}
}

// Write sends keystrokes to the remote stdin.
func (s *SessionService) Write(sid, data string) error {
	sess, err := s.get(sid)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	sh := sess.shell
	sess.mu.Unlock()
	if sh == nil {
		return errors.New("session is not connected")
	}
	s.touchVault()
	_, err = sh.Write([]byte(data))
	return err
}

// Resize applies a new terminal size; sizes requested while connecting are used at PTY creation.
func (s *SessionService) Resize(sid string, cols, rows int) error {
	if cols < 1 || rows < 1 {
		return errors.New("invalid size")
	}
	sess, err := s.get(sid)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	sess.cols, sess.rows = cols, rows
	sh := sess.shell
	sess.mu.Unlock()
	if sh == nil {
		return nil
	}
	return sh.Resize(cols, rows)
}

// Reconnect re-runs the connection for a session whose link dropped (user pressed Enter).
// It never happens automatically so commands are not replayed by surprise.
func (s *SessionService) Reconnect(sid string, cols, rows int) error {
	sess, err := s.get(sid)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	if sess.shell != nil {
		sess.mu.Unlock()
		return errors.New("session is still connected")
	}
	if cols > 0 && rows > 0 {
		sess.cols, sess.rows = cols, rows
	}
	sess.closed = false
	sess.abort = make(chan struct{})
	sess.mu.Unlock()
	go s.run(sess)
	return nil
}

// Close ends the session, its forwards and any connection in progress.
func (s *SessionService) Close(sid string) error {
	s.mu.Lock()
	sess, ok := s.sessions[sid]
	delete(s.sessions, sid)
	s.mu.Unlock()
	if !ok {
		return errors.New("unknown session")
	}
	if s.OnClose != nil {
		defer s.OnClose(sid)
	}
	sess.mu.Lock()
	sess.closed = true
	select {
	case <-sess.abort:
	default:
		close(sess.abort)
	}
	sh := sess.shell
	lg := sess.log
	sess.mu.Unlock()
	if lg != nil {
		defer lg.Close("closed by you")
	}
	s.f.stopOwned(sid)
	s.c.prompts.clear(sid)
	if sh != nil {
		sh.Close()
	}
	return nil
}

// TrustHostKey answers a term:hostkey:<key> event. The fingerprint must equal the one displayed.
// Passing an empty fingerprint rejects the key.
func (s *SessionService) TrustHostKey(sid, fingerprint string) error {
	return s.c.prompts.answer(sid, fingerprint, fingerprint != "")
}

// AnswerAuth answers a term:auth:<key> event: one answer for each question it asked, in the same order. Passing nil
// cancels the sign-in.
func (s *SessionService) AnswerAuth(key string, answers []string) error {
	return s.c.authPrompts.answer(key, answers)
}

// activeClient returns a connected client for hostID, if a terminal session is open on it.
func (s *SessionService) activeClient(hostID string) (*sshx.Client, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sid, x := range s.sessions {
		x.mu.Lock()
		cl, ok := x.client, x.hostID == hostID
		x.mu.Unlock()
		if ok && cl != nil {
			return cl, sid
		}
	}
	return nil, ""
}

// CloseAll terminates every session (app shutdown).
func (s *SessionService) CloseAll() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		_ = s.Close(id)
	}
}

// batcher collects terminal output for a short while and sends it to the frontend in few events; until the
// frontend attached, it is kept (up to maxPending) and replayed by Attach.
func (s *SessionService) batcher(sess *session) *sshx.Batcher {
	lg := s.c.sessionLogFor(sess)
	sess.mu.Lock()
	old := sess.log
	sess.log = lg
	sess.mu.Unlock()
	if old != nil {
		old.Close("reconnected")
	}
	return sshx.NewBatcher(outputBatchInterval, outputBatchMax, func(p []byte) {
		if lg != nil {
			lg.Write(p)
		}
		s.touchVault()
		sess.mu.Lock()
		defer sess.mu.Unlock()
		sess.appendScreen(p)
		if !sess.attached {
			if len(sess.pend)+len(p) <= maxPending {
				sess.pend = append(sess.pend, p...)
			}
			return
		}
		sess.gate.Sent(len(p))
		s.c.emit("term:data:"+sess.sid, base64.StdEncoding.EncodeToString(p))
	}).WithGate(sess.gate)
}

// Ack tells the backend that the terminal has parsed total bytes of this session's output since it attached. Output
// is only read from the connection while less than flowWindow is unparsed, so a program that writes faster than the
// terminal can draw (yes, cat of a huge file) slows down instead of freezing the window.
func (s *SessionService) Ack(sid string, total int64) error {
	sess, err := s.get(sid)
	if err != nil {
		return err
	}
	sess.gate.Ack(total)
	return nil
}

func (s *SessionService) markClosed(sess *session, reason string, clean bool) {
	s.logEnd(sess, reason, clean)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.state = "closed"
	sess.closedEv = map[string]any{"reason": reason, "clean": clean}
	s.c.emit("term:closed:"+sess.sid, sess.closedEv)
}

// cwder is a shell that can tell which folder it is in (a shell on this computer).
type cwder interface{ Cwd() string }

// remoteCwdCommand finds the login shell of this very connection on a Linux server and prints its folder. The shell
// has the connection's SSH_CONNECTION and a terminal (SSH_TTY), and it leads its own session (session id = pid); the
// command running this script has no terminal, so it never matches itself. Anything else prints nothing.
const remoteCwdCommand = `c="$SSH_CONNECTION"; [ -n "$c" ] || exit 0
for d in /proc/[0-9]*; do
  p=${d#/proc/}
  e=$(tr '\0' '\n' < "$d/environ" 2>/dev/null) || continue
  printf '%s\n' "$e" | grep -qFx "SSH_CONNECTION=$c" || continue
  printf '%s\n' "$e" | grep -q '^SSH_TTY=' || continue
  [ "$(sed 's/^.*) //' "$d/stat" 2>/dev/null | cut -d' ' -f4)" = "$p" ] || continue
  readlink "$d/cwd" && exit 0
done`

// Cwd is the folder the shell of a session is in, or "" when that cannot be told: a shell on this computer is asked
// directly, a Linux server through its /proc. The panel's OSC 7 (the shell reporting its own folder) covers the rest.
func (s *SessionService) Cwd(sid string) (string, error) {
	sess, err := s.get(sid)
	if err != nil {
		return "", err
	}
	sess.mu.Lock()
	sh, cl := sess.shell, sess.client
	sess.mu.Unlock()
	if c, ok := sh.(cwder); ok {
		return c.Cwd(), nil
	}
	if cl == nil {
		return "", nil
	}
	out, _ := cl.Exec(remoteCwdCommand, 5*time.Second)
	if dir := strings.TrimSpace(out); strings.HasPrefix(dir, "/") && !strings.ContainsAny(dir, "\r\n") {
		return dir, nil
	}
	return "", nil
}
