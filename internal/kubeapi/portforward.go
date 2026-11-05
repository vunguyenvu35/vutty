package kubeapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// PortForward opens one TCP connection to a port of a pod, through the API server, over the same channel protocol as exec (the one
// the kubelet speaks for port-forward since long before kubectl moved to WebSocket): for the one port asked, channel 0 carries the
// data and channel 1 the errors, and the first frame of each is the port number in two bytes. Every connection from the local
// listener is one call of this, as kubectl does.
func (c *Client) PortForward(ctx context.Context, ns, pod string, port int) (net.Conn, error) {
	if ns == "" || pod == "" || port < 1 || port > 65535 {
		return nil, errors.New("a namespace, a pod and a port are needed")
	}
	ws, err := c.dialSocket(ctx, "/api/v1/namespaces/"+url.PathEscape(ns)+"/pods/"+url.PathEscape(pod)+"/portforward", url.Values{"ports": {strconv.Itoa(port)}})
	if err != nil {
		return nil, err
	}
	if ws.Subprotocol() != execProtocol {
		_ = ws.Close()
		return nil, errors.New("the server does not support port-forward over WebSocket")
	}
	pr, pw := io.Pipe()
	p := &pfConn{ws: ws, pr: pr, pw: pw, port: port, remote: pod + ":" + strconv.Itoa(port), done: make(chan struct{})}
	go p.read()
	go p.ping()
	return p, nil
}

// pfConn is that connection as a net.Conn.
type pfConn struct {
	ws     *websocket.Conn
	pr     *io.PipeReader
	pw     *io.PipeWriter
	port   int
	remote string
	wmu    sync.Mutex
	once   sync.Once
	done   chan struct{}
}

type pfAddr string

func (a pfAddr) Network() string { return "k8s" }
func (a pfAddr) String() string  { return string(a) }

func (p *pfConn) read() {
	defer p.shutdown()
	gotData, gotErr := false, false
	var errText strings.Builder
	for {
		_, msg, err := p.ws.ReadMessage()
		if err != nil {
			select {
			case <-p.done: // closed on this side: that is an end, not a failure
				_ = p.pw.Close()
				return
			default:
			}
			if errText.Len() > 0 {
				_ = p.pw.CloseWithError(errors.New(strings.TrimSpace(errText.String())))
			} else if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
				_ = p.pw.Close()
			} else {
				_ = p.pw.CloseWithError(err)
			}
			return
		}
		if len(msg) == 0 {
			continue
		}
		body := msg[1:]
		switch msg[0] {
		case 0:
			if !gotData { // the first frame of a channel is the port number
				gotData = true
				if len(body) <= 2 {
					continue
				}
				body = body[2:]
			}
			if len(body) > 0 {
				if _, err := p.pw.Write(body); err != nil {
					return
				}
			}
		case 1:
			if !gotErr {
				gotErr = true
				if len(body) <= 2 {
					continue
				}
				body = body[2:]
			}
			errText.Write(body)
			if errText.Len() > 0 { // what the pod says on the error channel ends the connection (the port is closed there, say)
				_ = p.pw.CloseWithError(fmt.Errorf("port %d of the pod: %s", p.port, strings.TrimSpace(errText.String())))
				_ = p.ws.Close()
				return
			}
		}
	}
}

func (p *pfConn) ping() {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-t.C:
		}
		p.wmu.Lock()
		err := p.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
		p.wmu.Unlock()
		if err != nil {
			return
		}
	}
}

func (p *pfConn) Read(b []byte) (int, error) { return p.pr.Read(b) }

func (p *pfConn) Write(b []byte) (int, error) {
	for off := 0; off < len(b); off += maxFrame {
		end := min(off+maxFrame, len(b))
		p.wmu.Lock()
		_ = p.ws.SetWriteDeadline(time.Now().Add(30 * time.Second))
		err := p.ws.WriteMessage(websocket.BinaryMessage, append([]byte{0}, b[off:end]...))
		p.wmu.Unlock()
		if err != nil {
			return off, err
		}
	}
	return len(b), nil
}

func (p *pfConn) Close() error {
	p.wmu.Lock()
	_ = p.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(2*time.Second))
	p.wmu.Unlock()
	p.shutdown()
	return nil
}

// shutdown ends the connection once: the WebSocket, the pipe and the keep-alive.
func (p *pfConn) shutdown() {
	p.once.Do(func() {
		close(p.done)
		_ = p.ws.Close()
		_ = p.pw.Close()
	})
}

func (p *pfConn) LocalAddr() net.Addr                { return pfAddr("local") }
func (p *pfConn) RemoteAddr() net.Addr               { return pfAddr(p.remote) }
func (p *pfConn) SetDeadline(t time.Time) error      { return nil }
func (p *pfConn) SetReadDeadline(t time.Time) error  { return nil }
func (p *pfConn) SetWriteDeadline(t time.Time) error { return nil }
