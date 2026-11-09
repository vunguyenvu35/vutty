package usecase

import (
	"errors"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	followMax      = 3 // streams open at once on one connection
	followBatch    = 250 * time.Millisecond
	followPending  = 5000 // lines waiting for the screen; the oldest are dropped when it cannot keep up
	followLineMax  = 4096 // a longer line is cut
	followKeyShape = `^[A-Za-z0-9-]{8,64}$`
)

var followKey = regexp.MustCompile(followKeyShape)

// logFollow is a log being read as it grows: a command that does not end, on a session of its own, whose lines go to the
// screen in batches (event server:log:<key>), until the screen stops it, the command ends or the connection is lost.
type logFollow struct {
	halt func() // ends what feeds the log: a command on the host, or a request to the API server
	done chan struct{}
	once sync.Once
}

func (f *logFollow) stop() { f.once.Do(f.halt) }

// sshFollow is a followed log read by a command on the host, on a session of its own.
func sshFollow(sess *ssh.Session, stdin io.WriteCloser) *logFollow {
	f := &logFollow{done: make(chan struct{})}
	f.halt = func() {
		_ = stdin.Close() // the script on the host takes the end of its input as the sign to stop the log
		_ = sess.Signal(ssh.SIGTERM)
		_ = sess.Close()
	}
	go func() { _ = sess.Wait(); close(f.done) }()
	return f
}

// followScript runs the log command in a job of its own, next to a watcher of the standard input: when the panel closes the
// session, or the connection drops, the input ends and the watcher kills the whole job (job control gives it a process group, so
// a docker or journalctl under a subshell goes too, and does not stay on the host until the next line it cannot write).
func followScript(cmd string) string {
	return asRoot + "LC_ALL=C; export LC_ALL; set -m\n(\n" + cmd + "\n) & P=$!\n(cat >/dev/null; kill -TERM -- -$P 2>/dev/null || kill $P 2>/dev/null) <&0 &\nW=$!\nwait $P; R=$?; kill $W 2>/dev/null; exit $R"
}

// lineBatcher splits what a command prints into lines and hands them over in batches.
type lineBatcher struct {
	mu      sync.Mutex
	part    string
	lines   []string
	dropped int
}

func (b *lineBatcher) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.part += strings.ReplaceAll(string(p), "\r", "")
	for {
		i := strings.IndexByte(b.part, '\n')
		if i < 0 {
			break
		}
		l := b.part[:i]
		b.part = b.part[i+1:]
		if len(l) > followLineMax {
			l = l[:followLineMax] + "…"
		}
		b.lines = append(b.lines, l)
	}
	if len(b.part) > followLineMax*4 { // a line that never ends
		b.lines = append(b.lines, b.part[:followLineMax]+"…")
		b.part = ""
	}
	if over := len(b.lines) - followPending; over > 0 {
		b.lines = b.lines[over:]
		b.dropped += over
	}
	return len(p), nil
}

func (b *lineBatcher) take(final bool) ([]string, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if final && b.part != "" {
		b.lines = append(b.lines, b.part)
		b.part = ""
	}
	l, d := b.lines, b.dropped
	b.lines, b.dropped = nil, 0
	return l, d
}

// LogsFollowStart starts reading a log as it grows. key is chosen by the screen, which listens to server:log:<key> (a batch:
// lines and how many were dropped) and server:logend:<key> (why it ended) before it asks: nothing is lost in between. The log
// starts with its last `lines` lines. Time ranges and boots are not followed.
func (s *ServerService) LogsFollowStart(sid, key, kind, name string, lines int, since string) error {
	if !followKey.MatchString(key) {
		return errors.New("a key of 8 to 64 letters, digits or dashes is required")
	}
	q, err := logQueryOf(lines, since, "", "", false)
	if err != nil {
		return err
	}
	q.follow = true
	cmd, err := logCommand(kind, name, q)
	if err != nil {
		return err
	}
	return s.startFollow(sid, key, func(x *serverSession, out *lineBatcher) (*logFollow, error) {
		if x.client == nil {
			return nil, errors.New("this is a cluster reached with a kubeconfig: there is no server to run that on")
		}
		return s.followOnHost(x, out, followScript(cmd))
	})
}

// followOnHost starts a script that does not end on a session of its own, and sends what it prints to out.
func (s *ServerService) followOnHost(x *serverSession, out *lineBatcher, script string) (*logFollow, error) {
	sess, err := x.client.NewSession()
	if err != nil {
		return nil, err
	}
	sess.Stdout, sess.Stderr = out, out
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	if err := sess.Start(script); err != nil {
		_ = sess.Close()
		return nil, err
	}
	return sshFollow(sess, stdin), nil
}

// startFollow registers a followed log under key and sends its lines to the screen in batches. start makes what feeds it (it runs with
// the limit already checked, and returns the follow whose done is closed when the feed ends).
func (s *ServerService) startFollow(sid, key string, start func(x *serverSession, out *lineBatcher) (*logFollow, error)) error {
	x, err := s.get(sid)
	if err != nil {
		return err
	}
	x.fmu.Lock()
	if len(x.follows) >= followMax {
		x.fmu.Unlock()
		return errors.New("three logs are followed already: stop one first")
	}
	if _, dup := x.follows[key]; dup {
		x.fmu.Unlock()
		return errors.New("that log is followed already")
	}
	out := &lineBatcher{}
	f, err := start(x, out)
	if err != nil {
		x.fmu.Unlock()
		return err
	}
	if x.follows == nil {
		x.follows = map[string]*logFollow{}
	}
	x.follows[key] = f
	x.fmu.Unlock()
	go s.pumpFollow(x, key, f, out)
	return nil
}

// pumpFollow hands the lines to the screen every quarter of a second, until the command ends.
func (s *ServerService) pumpFollow(x *serverSession, key string, f *logFollow, out *lineBatcher) {
	tick := time.NewTicker(followBatch)
	defer tick.Stop()
	flush := func(final bool) {
		if l, d := out.take(final); len(l) > 0 || d > 0 {
			s.c.emit("server:log:"+key, map[string]any{"lines": l, "dropped": d})
		}
	}
	for {
		select {
		case <-tick.C:
			flush(false)
		case <-f.done:
			flush(true)
			x.fmu.Lock()
			if x.follows[key] == f {
				delete(x.follows, key)
			}
			x.fmu.Unlock()
			f.stop()
			s.c.emit("server:logend:"+key, map[string]any{"reason": "ended"})
			return
		}
	}
}

// LogsFollowStop ends a followed log. A key that is not (or no longer) followed is not an error.
func (s *ServerService) LogsFollowStop(sid, key string) error {
	x, err := s.get(sid)
	if err != nil {
		return nil
	}
	x.fmu.Lock()
	f := x.follows[key]
	x.fmu.Unlock()
	if f != nil {
		f.stop()
	}
	return nil
}

func (x *serverSession) stopFollows() {
	x.fmu.Lock()
	all := make([]*logFollow, 0, len(x.follows))
	for _, f := range x.follows {
		all = append(all, f)
	}
	x.fmu.Unlock()
	for _, f := range all {
		f.stop()
	}
}
