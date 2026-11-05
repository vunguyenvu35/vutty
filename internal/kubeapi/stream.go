package kubeapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// execProtocol is the WebSocket sub-protocol of exec that every API server speaks: a binary frame is a channel byte and then data
// (0 stdin, 1 stdout, 2 stderr, 3 the final status as JSON, 4 the size of the terminal as JSON).
const execProtocol = "v4.channel.k8s.io"

const (
	chStdin  = 0
	chStdout = 1
	chStderr = 2
	chStatus = 3
	chResize = 4

	// maxFrame is the most that is typed in one frame.
	maxFrame = 32 << 10
	// pingEvery keeps a proxy (Rancher, a load balancer) from closing a shell that nobody is typing in.
	pingEvery = 25 * time.Second
)

// Stream is a running shell in a pod. Output goes to the callback given to Exec; keystrokes come in through Write.
type Stream struct {
	conn *websocket.Conn
	wmu  sync.Mutex // one writer at a time
	done chan struct{}
	once sync.Once

	mu  sync.Mutex
	err error
}

// Exec runs a command in a container of a pod with a terminal (a shell, usually) and returns at once; the output of the
// command (stdout and stderr are one stream with a terminal) is passed to onData until the command ends.
func (c *Client) Exec(ctx context.Context, ns, pod, container string, command []string, cols, rows int, onData func([]byte)) (*Stream, error) {
	conn, err := c.dialExec(ctx, ns, pod, container, command, true, true)
	if err != nil {
		return nil, err
	}
	s := &Stream{conn: conn, done: make(chan struct{})}
	_ = s.Resize(cols, rows)
	go s.read(onData)
	go s.ping()
	return s, nil
}

func (s *Stream) read(onData func([]byte)) {
	defer s.finish(nil)
	for {
		_, msg, err := s.conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) && !s.closed() {
				s.setErr(err)
			}
			return
		}
		if len(msg) == 0 {
			continue
		}
		switch msg[0] {
		case chStdout, chStderr:
			if len(msg) > 1 {
				onData(append([]byte(nil), msg[1:]...))
			}
		case chStatus:
			s.setErr(statusError(msg[1:]))
			return
		}
	}
}

// statusError reads the final status of the command: nil when it ended (whatever its exit code: a shell that exits 1 is a shell
// that ended), the server's sentence when the command could not be run.
func statusError(b []byte) error {
	var st struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
	}
	if json.Unmarshal(b, &st) != nil || st.Status == "Success" || st.Reason == "NonZeroExitCode" || st.Status == "" {
		return nil
	}
	if st.Message == "" {
		return errors.New("the command failed: " + st.Reason)
	}
	return errors.New(st.Message)
}

func (s *Stream) ping() {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.wmu.Lock()
			err := s.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
			s.wmu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (s *Stream) frame(ch byte, p []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return s.conn.WriteMessage(websocket.BinaryMessage, append([]byte{ch}, p...))
}

// Write types p into the terminal.
func (s *Stream) Write(p []byte) (int, error) {
	if s.closed() {
		return 0, errors.New("the shell has ended")
	}
	for off := 0; off < len(p); off += maxFrame {
		end := min(off+maxFrame, len(p))
		if err := s.frame(chStdin, p[off:end]); err != nil {
			return off, err
		}
	}
	return len(p), nil
}

// Resize tells the pod the size of the terminal.
func (s *Stream) Resize(cols, rows int) error {
	if cols < 1 || rows < 1 {
		return nil
	}
	b, _ := json.Marshal(map[string]int{"Width": cols, "Height": rows})
	return s.frame(chResize, b)
}

// Done is closed when the shell has ended; Err then says why (nil for a normal end).
func (s *Stream) Done() <-chan struct{} { return s.done }

func (s *Stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Stream) setErr(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

func (s *Stream) closed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *Stream) finish(err error) {
	s.setErr(err)
	s.once.Do(func() {
		close(s.done)
		_ = s.conn.Close()
	})
}

// Close ends the shell.
func (s *Stream) Close() {
	s.wmu.Lock()
	_ = s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	s.wmu.Unlock()
	s.finish(nil)
}

// dialExec opens the WebSocket of an exec call: with a terminal (tty, stdout and stderr come as one) or without one, and with the
// channel of the keyboard (stdin) or without it.
func (c *Client) dialExec(ctx context.Context, ns, pod, container string, command []string, tty, stdin bool) (*websocket.Conn, error) {
	if ns == "" || pod == "" || len(command) == 0 {
		return nil, errors.New("a namespace, a pod and a command are needed")
	}
	q := url.Values{"stdin": {strconv.FormatBool(stdin)}, "stdout": {"true"}, "stderr": {"true"}, "tty": {strconv.FormatBool(tty)}}
	for _, w := range command {
		q.Add("command", w)
	}
	if container != "" {
		q.Set("container", container)
	}
	conn, err := c.dialSocket(ctx, "/api/v1/namespaces/"+url.PathEscape(ns)+"/pods/"+url.PathEscape(pod)+"/exec", q)
	if err != nil {
		return nil, err
	}
	if conn.Subprotocol() != execProtocol {
		_ = conn.Close()
		return nil, errors.New("the server does not support a shell over WebSocket")
	}
	return conn, nil
}

// dialSocket opens a WebSocket on a path of the API server with the channel protocol (exec and port-forward both speak it).
func (c *Client) dialSocket(ctx context.Context, path string, q url.Values) (*websocket.Conn, error) {
	u, err := url.Parse(c.URL(path))
	if err != nil {
		return nil, err
	}
	u.RawQuery = q.Encode()
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	h, err := c.Header(ctx)
	if err != nil {
		return nil, err
	}
	// a WebSocket is HTTP/1.1: the TLS set-up of the HTTP client offers h2, which a server then picks and the upgrade fails
	tc := c.TLSConfig()
	tc.NextProtos = []string{"http/1.1"}
	d := websocket.Dialer{TLSClientConfig: tc, Subprotocols: []string{execProtocol}, HandshakeTimeout: 15 * time.Second, Proxy: c.proxy}
	conn, resp, err := d.DialContext(ctx, u.String(), h)
	if err != nil {
		if resp != nil { // the server answered the upgrade with a refusal: it says why in its own words
			b := make([]byte, 4096)
			n, _ := resp.Body.Read(b)
			_ = resp.Body.Close()
			return nil, apiError(resp.StatusCode, b[:n])
		}
		return nil, err
	}
	return conn, nil
}

// RunResult is how a command that was run without a terminal ended: its exit code, and the start of what it wrote to stderr.
type RunResult struct {
	ExitCode int
	Stderr   string
}

const maxRunStderr = 4 << 10

// Run runs a command in a container of a pod without a terminal and waits for it to end. What it writes to stdout goes to w: a
// write that fails (a size limit) ends the command with that error. Nothing is typed into it.
func (c *Client) Run(ctx context.Context, ns, pod, container string, command []string, stdout io.Writer) (RunResult, error) {
	conn, err := c.dialExec(ctx, ns, pod, container, command, false, false)
	if err != nil {
		return RunResult{}, err
	}
	defer conn.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() { // a cancelled context ends the read below
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stop:
		}
	}()
	var res RunResult
	var stderr []byte
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
				res.Stderr = string(stderr)
				return res, nil
			}
			return res, err
		}
		if len(msg) == 0 {
			continue
		}
		switch msg[0] {
		case chStdout:
			if _, err := stdout.Write(msg[1:]); err != nil {
				return res, err
			}
		case chStderr:
			if room := maxRunStderr - len(stderr); room > 0 {
				stderr = append(stderr, msg[1:min(len(msg), 1+room)]...)
			}
		case chStatus:
			res.Stderr = string(stderr)
			res.ExitCode, err = exitStatus(msg[1:])
			return res, err
		}
	}
}

// exitStatus reads the final status of a command: its exit code, or the server's sentence when it could not be run.
func exitStatus(b []byte) (int, error) {
	var st struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
		Details struct {
			Causes []struct {
				Reason  string `json:"reason"`
				Message string `json:"message"`
			} `json:"causes"`
		} `json:"details"`
	}
	if json.Unmarshal(b, &st) != nil || st.Status == "Success" || st.Status == "" {
		return 0, nil
	}
	if st.Reason == "NonZeroExitCode" {
		for _, c := range st.Details.Causes {
			if c.Reason == "ExitCode" {
				if n, err := strconv.Atoi(c.Message); err == nil {
					return n, nil
				}
			}
		}
		return 1, nil
	}
	if st.Message == "" {
		return 0, errors.New("the command failed: " + st.Reason)
	}
	return 0, errors.New(st.Message)
}
