package kubeapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// execServer is an API server that answers /exec like the real one: it echoes stdin on stdout, remembers the size of the terminal,
// and ends the command when it reads "exit".
type execServer struct {
	*httptest.Server
	mu     sync.Mutex
	query  url.Values
	auth   string
	resize []map[string]int
	status string // what the server sends when the command ends
}

func newExecServer(t *testing.T, protocol string) *execServer {
	return newExecServerH2(t, protocol, false)
}

// newExecServerH2 can offer HTTP/2 as the servers of real clusters do.
func newExecServerH2(t *testing.T, protocol string, h2 bool) *execServer {
	t.Helper()
	e := &execServer{status: `{"status":"Success"}`}
	up := websocket.Upgrader{Subprotocols: []string{protocol}}
	e.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/exec") {
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("container") == "forbidden" {
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"kind":"Status","message":"pods \"api\" is forbidden: User \"u\" cannot create resource \"pods/exec\""}`))
			return
		}
		e.mu.Lock()
		e.query, e.auth = r.URL.Query(), r.Header.Get("Authorization")
		e.mu.Unlock()
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			switch msg[0] {
			case 0:
				if strings.TrimSpace(string(msg[1:])) == "exit" {
					_ = c.WriteMessage(websocket.BinaryMessage, append([]byte{3}, e.status...))
					return
				}
				_ = c.WriteMessage(websocket.BinaryMessage, append([]byte{1}, msg[1:]...))
				if strings.HasPrefix(string(msg[1:]), "err") {
					_ = c.WriteMessage(websocket.BinaryMessage, []byte("\x02oops"))
				}
			case 4:
				var sz map[string]int
				_ = json.Unmarshal(msg[1:], &sz)
				e.mu.Lock()
				e.resize = append(e.resize, sz)
				e.mu.Unlock()
			}
		}
	}))
	e.Server.EnableHTTP2 = h2
	e.Server.StartTLS()
	t.Cleanup(e.Close)
	return e
}

type collector struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (c *collector) add(p []byte) { c.mu.Lock(); c.buf.Write(p); c.mu.Unlock() }
func (c *collector) text() string { c.mu.Lock(); defer c.mu.Unlock(); return c.buf.String() }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestExecOpensAShellInAPodAndMovesBytesBothWays(t *testing.T) {
	e := newExecServer(t, execProtocol)
	cl := open(t, kubeconfig(e.Server, "    token: s3cret"), Options{})
	var out collector
	s, err := cl.Exec(context.Background(), "shop", "api-1", "web", []string{"sh", "-c", "exec bash"}, 100, 30, out.add)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Write([]byte("ls\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the echo", func() bool { return out.text() == "ls\n" })
	if _, err := s.Write([]byte("err\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stderr, which a terminal shows with stdout", func() bool { return strings.Contains(out.text(), "oops") })
	if err := s.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the sizes", func() bool { e.mu.Lock(); defer e.mu.Unlock(); return len(e.resize) == 2 })
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.resize[0]["Width"] != 100 || e.resize[0]["Height"] != 30 || e.resize[1]["Width"] != 120 {
		t.Fatalf("sizes = %v", e.resize)
	}
	if e.auth != "Bearer s3cret" {
		t.Fatalf("auth = %q", e.auth)
	}
	q := e.query
	if q.Get("tty") != "true" || q.Get("stdin") != "true" || q.Get("container") != "web" || strings.Join(q["command"], "|") != "sh|-c|exec bash" {
		t.Fatalf("query = %v", q)
	}
}

func TestExecEndsWhenTheCommandEndsAndAFailureCarriesTheServersWords(t *testing.T) {
	e := newExecServer(t, execProtocol)
	cl := open(t, kubeconfig(e.Server, "    token: x"), Options{})
	var out collector
	s, err := cl.Exec(context.Background(), "shop", "api-1", "", []string{"sh"}, 80, 24, out.add)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Write([]byte("exit\n"))
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the shell did not end")
	}
	if s.Err() != nil {
		t.Fatalf("a normal end is no error: %v", s.Err())
	}
	if _, err := s.Write([]byte("x")); err == nil {
		t.Fatal("typing into an ended shell must fail")
	}

	e.status = `{"status":"Failure","message":"command terminated with exit code 137","reason":"NonZeroExitCode"}`
	s2, _ := cl.Exec(context.Background(), "shop", "api-1", "", []string{"sh"}, 80, 24, out.add)
	_, _ = s2.Write([]byte("exit\n"))
	<-s2.Done()
	if s2.Err() != nil {
		t.Fatalf("a shell that exits with a code has ended, not failed: %v", s2.Err())
	}

	e.status = `{"status":"Failure","message":"error executing command in container: no such file: \"bash\"","reason":"InternalError"}`
	s3, _ := cl.Exec(context.Background(), "shop", "api-1", "", []string{"sh"}, 80, 24, out.add)
	_, _ = s3.Write([]byte("exit\n"))
	<-s3.Done()
	if s3.Err() == nil || !strings.Contains(s3.Err().Error(), "no such file") {
		t.Fatalf("err = %v", s3.Err())
	}
}

func TestExecRefusedAtTheUpgradeSaysWhy(t *testing.T) {
	e := newExecServer(t, execProtocol)
	cl := open(t, kubeconfig(e.Server, "    token: x"), Options{})
	_, err := cl.Exec(context.Background(), "shop", "api-1", "forbidden", []string{"sh"}, 80, 24, func([]byte) {})
	if !IsForbidden(err) || !strings.Contains(err.Error(), "pods/exec") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecNeedsTheProtocolEveryServerSpeaks(t *testing.T) {
	e := newExecServer(t, "something.else")
	cl := open(t, kubeconfig(e.Server, "    token: x"), Options{})
	if _, err := cl.Exec(context.Background(), "shop", "api-1", "", []string{"sh"}, 80, 24, func([]byte) {}); err == nil || !strings.Contains(err.Error(), "WebSocket") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecRefusesWhatIsMissing(t *testing.T) {
	cl := open(t, kubeconfig(nil, "    token: x"), Options{})
	for _, c := range []struct {
		ns, pod string
		cmd     []string
	}{{"", "p", []string{"sh"}}, {"n", "", []string{"sh"}}, {"n", "p", nil}} {
		if _, err := cl.Exec(context.Background(), c.ns, c.pod, "", c.cmd, 80, 24, func([]byte) {}); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
}

// Real API servers (and Rancher in front of them) offer HTTP/2: a client that offers it too gets it, and cannot upgrade to WebSocket.
func TestExecWorksWithAServerThatOffersHTTP2(t *testing.T) {
	e := newExecServerH2(t, execProtocol, true)
	cl := open(t, kubeconfig(e.Server, "    token: x"), Options{})
	_, _ = cl.Get(context.Background(), "/version") // the HTTP client sets up its TLS (and adds h2) when it is first used, as it has by the time a shell is opened
	var out collector
	s, err := cl.Exec(context.Background(), "shop", "api-1", "", []string{"sh"}, 80, 24, out.add)
	if err != nil {
		t.Fatalf("exec against a server that offers h2: %v", err)
	}
	defer s.Close()
	_, _ = s.Write([]byte("hi\n"))
	waitFor(t, "the echo", func() bool { return out.text() == "hi\n" })
}
