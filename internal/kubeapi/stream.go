package kubeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
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
	if ns == "" || pod == "" || len(command) == 0 {
		return nil, errors.New("a namespace, a pod and a command are needed")
	}
	q := url.Values{"stdin": {"true"}, "stdout": {"true"}, "stderr": {"true"}, "tty": {"true"}}
	for _, w := range command {
		q.Add("command", w)
	}
	if container != "" {
		q.Set("container", container)
	}
	u, err := url.Parse(c.URL("/api/v1/namespaces/" + url.PathEscape(ns) + "/pods/" + url.PathEscape(pod) + "/exec"))
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
	if conn.Subprotocol() != execProtocol {
		_ = conn.Close()
		return nil, errors.New("the server does not support a shell over WebSocket")
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
