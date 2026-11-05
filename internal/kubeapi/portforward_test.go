package kubeapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// pfServer speaks port-forward as the kubelet does: the port number first on the data and the error channel, then the data
// (an upper-case echo), and an error on channel 1 when the port is "closed".
type pfServer struct {
	*httptest.Server
	mu    sync.Mutex
	query string
	path  string
	auth  string
}

func newPFServer(t *testing.T, protocol string, closedPort string) *pfServer {
	t.Helper()
	p := &pfServer{}
	up := websocket.Upgrader{Subprotocols: []string{protocol}}
	p.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.query, p.path, p.auth = r.URL.RawQuery, r.URL.Path, r.Header.Get("Authorization")
		p.mu.Unlock()
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		port := []byte{byte(8080 & 0xff), byte(8080 >> 8)}
		_ = c.WriteMessage(websocket.BinaryMessage, append([]byte{0}, port...))
		_ = c.WriteMessage(websocket.BinaryMessage, append([]byte{1}, port...))
		if r.URL.Query().Get("ports") == closedPort {
			_ = c.WriteMessage(websocket.BinaryMessage, append([]byte{1}, "connection refused"...))
			return
		}
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if msg[0] == 0 {
				_ = c.WriteMessage(websocket.BinaryMessage, append([]byte{0}, strings.ToUpper(string(msg[1:]))...))
			}
		}
	}))
	t.Cleanup(p.Close)
	return p
}

func TestPortForwardMovesBytesBothWaysAndDropsThePortFrames(t *testing.T) {
	s := newPFServer(t, execProtocol, "")
	cl := open(t, kubeconfig(s.Server, "    token: s3cret"), Options{})
	c, err := cl.PortForward(context.Background(), "shop", "api-1", 8080)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "HELLO" {
		t.Fatalf("%q %v: the two bytes of the port must not reach the reader", buf, err)
	}
	big := strings.Repeat("a", maxFrame*2+10)
	go func() { _, _ = c.Write([]byte(big)) }()
	got := make([]byte, len(big))
	if _, err := io.ReadFull(c, got); err != nil || string(got) != strings.ToUpper(big) {
		t.Fatalf("a long write is cut in frames and comes back whole: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path != "/api/v1/namespaces/shop/pods/api-1/portforward" || s.query != "ports=8080" || s.auth != "Bearer s3cret" {
		t.Fatalf("%q %q %q", s.path, s.query, s.auth)
	}
}

func TestPortForwardSaysWhatThePodSaysWhenThePortIsClosed(t *testing.T) {
	s := newPFServer(t, execProtocol, "9")
	cl := open(t, kubeconfig(s.Server, "    token: x"), Options{})
	c, err := cl.PortForward(context.Background(), "shop", "api-1", 9)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	done := make(chan error, 1)
	go func() { _, err := c.Read(make([]byte, 8)); done <- err }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "connection refused") || !strings.Contains(err.Error(), "port 9") {
			t.Fatalf("%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the read did not end")
	}
}

func TestPortForwardRefusals(t *testing.T) {
	s := newPFServer(t, "other.protocol", "")
	cl := open(t, kubeconfig(s.Server, "    token: x"), Options{})
	if _, err := cl.PortForward(context.Background(), "shop", "api-1", 8080); err == nil {
		t.Fatal("a server that does not speak the protocol was used")
	}
	for _, bad := range []struct {
		ns, pod string
		port    int
	}{{"", "p", 80}, {"n", "", 80}, {"n", "p", 0}, {"n", "p", 70000}} {
		if _, err := cl.PortForward(context.Background(), bad.ns, bad.pod, bad.port); err == nil {
			t.Errorf("%+v", bad)
		}
	}
}

func TestClosingAPortForwardEndsTheReaderAndTheServerSide(t *testing.T) {
	s := newPFServer(t, execProtocol, "")
	cl := open(t, kubeconfig(s.Server, "    token: x"), Options{})
	c, err := cl.PortForward(context.Background(), "shop", "api-1", 8080)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := c.Read(make([]byte, 8)); done <- err }()
	time.Sleep(50 * time.Millisecond)
	_ = c.Close()
	select {
	case err := <-done:
		if err != io.EOF {
			t.Fatalf("%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not end the read")
	}
	_ = c.Close() // twice is fine
}
