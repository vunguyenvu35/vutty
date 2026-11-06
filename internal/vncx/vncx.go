// Package vncx lets the web page of the app talk to a VNC server: it listens on this computer only, behind a secret path, and
// passes the bytes of one WebSocket connection to a TCP connection to the server (the way websockify does), for the noVNC viewer.
package vncx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Bridge is one open path to a VNC server. It accepts a single WebSocket connection: whoever knows its URL gets it, once.
type Bridge struct {
	URL string // ws://127.0.0.1:<port>/<secret>

	ln     net.Listener
	srv    *http.Server
	secret string
	dial   func() (net.Conn, error)

	mu     sync.Mutex
	used   bool
	conn   net.Conn
	ws     *websocket.Conn
	closed bool
	done   chan struct{}
	onEnd  func(error)
}

var upgrader = websocket.Upgrader{
	Subprotocols:    []string{"binary"},
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 32 << 10,
	// The page of the app is not served from this address, so the origin cannot be compared; the secret path is what lets it in, and
	// the listener is on the loopback only.
	CheckOrigin: func(*http.Request) bool { return true },
}

// Start listens on a free port of 127.0.0.1. dial opens the connection to the VNC server when the viewer connects; onEnd (may be
// nil) is told once when the viewer or the server hangs up, with nil for an ordinary end.
func Start(dial func() (net.Conn, error), onEnd func(error)) (*Bridge, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		_ = ln.Close()
		return nil, err
	}
	b := &Bridge{ln: ln, secret: hex.EncodeToString(raw[:]), dial: dial, done: make(chan struct{}), onEnd: onEnd}
	b.URL = "ws://" + ln.Addr().String() + "/" + b.secret
	b.srv = &http.Server{Handler: http.HandlerFunc(b.serve), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = b.srv.Serve(ln) }()
	return b, nil
}

func (b *Bridge) serve(w http.ResponseWriter, r *http.Request) {
	if strings.TrimPrefix(r.URL.Path, "/") != b.secret {
		http.NotFound(w, r)
		return
	}
	b.mu.Lock()
	if b.used || b.closed {
		b.mu.Unlock()
		http.Error(w, "already connected", http.StatusConflict)
		return
	}
	b.used = true
	b.mu.Unlock()

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		b.finish(err)
		return
	}
	conn, err := b.dial()
	if err != nil {
		_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, truncate(err.Error(), 100)), time.Now().Add(time.Second))
		_ = ws.Close()
		b.finish(err)
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		_ = ws.Close()
		_ = conn.Close()
		return
	}
	b.ws, b.conn = ws, conn
	b.mu.Unlock()

	errc := make(chan error, 2)
	go func() { // server -> viewer
		buf := make([]byte, 32<<10)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					errc <- werr
					return
				}
			}
			if err != nil {
				errc <- err
				return
			}
		}
	}()
	go func() { // viewer -> server
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				errc <- err
				return
			}
			if _, err := conn.Write(data); err != nil {
				errc <- err
				return
			}
		}
	}()
	err = <-errc
	if errors.Is(err, io.EOF) || websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
		err = nil
	}
	b.finish(err)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// finish ends the bridge (once) and tells onEnd why.
func (b *Bridge) finish(err error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	ws, conn := b.ws, b.conn
	b.mu.Unlock()
	if ws != nil {
		_ = ws.Close()
	}
	if conn != nil {
		_ = conn.Close()
	}
	_ = b.srv.Close()
	close(b.done)
	if b.onEnd != nil {
		b.onEnd(err)
	}
}

// Close ends the bridge and the connections under it. It is safe to call twice.
func (b *Bridge) Close() { b.finish(nil) }

// Done is closed when the bridge has ended.
func (b *Bridge) Done() <-chan struct{} { return b.done }
