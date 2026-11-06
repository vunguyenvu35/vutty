package vncx

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// echoServer is a TCP server that sends a banner and then echoes what it gets.
func echoServer(t *testing.T) (addr string, accepted chan net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted = make(chan net.Conn, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- c
			go func() { _, _ = c.Write([]byte("RFB 003.008\n")); _, _ = io.Copy(c, c) }()
		}
	}()
	return ln.Addr().String(), accepted
}

func dialer(addr string) func() (net.Conn, error) {
	return func() (net.Conn, error) { return net.DialTimeout("tcp", addr, 2*time.Second) }
}

func connect(t *testing.T, url string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	d := websocket.Dialer{Subprotocols: []string{"binary"}, HandshakeTimeout: 2 * time.Second}
	return d.Dial(url, nil)
}

func TestBridgePassesBytesBothWaysAndEndsWithTheViewer(t *testing.T) {
	addr, _ := echoServer(t)
	ended := make(chan error, 1)
	b, err := Start(dialer(addr), func(err error) { ended <- err })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	if !strings.HasPrefix(b.URL, "ws://127.0.0.1:") {
		t.Fatalf("%s", b.URL)
	}
	ws, _, err := connect(t, b.URL)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Subprotocol() != "binary" {
		t.Fatalf("noVNC asks for the binary subprotocol: %q", ws.Subprotocol())
	}
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	typ, msg, err := ws.ReadMessage()
	if err != nil || typ != websocket.BinaryMessage || string(msg) != "RFB 003.008\n" {
		t.Fatalf("%d %q %v", typ, msg, err)
	}
	payload := bytes.Repeat([]byte{0, 1, 2, 250}, 20000) // 80 kB: more than one read
	if err := ws.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for len(got) < len(payload) {
		_, m, err := ws.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, m...)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("what came back is not what was sent")
	}
	_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	select {
	case err := <-ended:
		if err != nil {
			t.Fatalf("a viewer that closes is an ordinary end: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the bridge did not end")
	}
	select {
	case <-b.Done():
	default:
		t.Fatal("Done is not closed")
	}
	if _, _, err := connect(t, b.URL); err == nil {
		t.Fatal("the listener is gone after the end")
	}
}

func TestOnlyTheSecretPathGetsIn_AndOnlyOnce(t *testing.T) {
	addr, _ := echoServer(t)
	b, err := Start(dialer(addr), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	base := b.URL[:strings.LastIndex(b.URL, "/")]
	for _, p := range []string{"/", "/wrong", "/" + strings.Repeat("0", 32)} {
		if _, resp, err := connect(t, base+p); err == nil || resp == nil || resp.StatusCode != http.StatusNotFound {
			t.Errorf("%q got in or gave %v", p, resp)
		}
	}
	ws, _, err := connect(t, b.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if _, resp, err := connect(t, b.URL); err == nil || resp == nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("a second viewer: %v %v", resp, err)
	}
}

func TestServerThatHangsUpEndsTheViewer(t *testing.T) {
	addr, accepted := echoServer(t)
	ended := make(chan error, 1)
	b, _ := Start(dialer(addr), func(err error) { ended <- err })
	t.Cleanup(b.Close)
	ws, _, err := connect(t, b.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	c := <-accepted
	_ = c.Close()
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			break // the viewer sees the end
		}
	}
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("the bridge did not end")
	}
}

func TestAServerThatCannotBeReachedIsToldToTheViewerAndTheCaller(t *testing.T) {
	ended := make(chan error, 1)
	b, _ := Start(func() (net.Conn, error) { return nil, errors.New("connection refused") }, func(err error) { ended <- err })
	t.Cleanup(b.Close)
	ws, _, err := connect(t, b.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = ws.ReadMessage()
	var ce *websocket.CloseError
	if !errors.As(err, &ce) || !strings.Contains(ce.Text, "connection refused") {
		t.Fatalf("the viewer is told why: %v", err)
	}
	if e := <-ended; e == nil || !strings.Contains(e.Error(), "connection refused") {
		t.Fatalf("%v", e)
	}
}

func TestCloseIsSafeTwiceAndStopsTheConnections(t *testing.T) {
	addr, accepted := echoServer(t)
	b, _ := Start(dialer(addr), nil)
	ws, _, err := connect(t, b.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	c := <-accepted
	b.Close()
	b.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 16)
	for {
		if _, err := c.Read(buf); err != nil {
			break
		}
	}
}
