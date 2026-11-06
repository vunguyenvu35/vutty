package usecase

import (
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"vutty/internal/domain"
)

// fakeBridge keeps the dial function the service gave it, so a test can play the viewer.
type fakeBridge struct {
	dial   func() (net.Conn, error)
	onEnd  func(error)
	mu     sync.Mutex
	closed bool
}

func (f *fakeBridge) Address() string { return "ws://127.0.0.1:1/secret" }
func (f *fakeBridge) Close()          { f.mu.Lock(); f.closed = true; f.mu.Unlock() }
func (f *fakeBridge) isClosed() bool  { f.mu.Lock(); defer f.mu.Unlock(); return f.closed }

func installVNC(h *harness) *fakeBridge {
	fb := &fakeBridge{}
	h.core.StartVNC = func(dial func() (net.Conn, error), onEnd func(error)) (VNCBridge, error) {
		fb.dial, fb.onEnd = dial, onEnd
		return fb, nil
	}
	return fb
}

// vncEcho is a TCP server that sends "RFB" and echoes what it gets.
func vncEcho(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = c.Write([]byte("RFB")); _, _ = io.Copy(c, c) }()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func readN(t *testing.T, c net.Conn, n int) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, n)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

func TestVNCDirectConnectsToTheHostAddress(t *testing.T) {
	h := newHarness(t).unlocked(t)
	fb := installVNC(h)
	port := vncEcho(t)
	host, err := h.app.Host.SaveHost(domain.Host{Label: "desk", Hostname: "127.0.0.1", VNCEnabled: true, VNCPort: port})
	if err != nil {
		t.Fatal(err)
	}
	info, err := h.app.Vnc.Open(host.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.URL != "ws://127.0.0.1:1/secret" || info.Host != "desk" || info.SID == "" {
		t.Fatalf("%+v", info)
	}
	c, err := fb.dial()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := readN(t, c, 3); got != "RFB" {
		t.Fatalf("%q", got)
	}
	if e := h.waitLog(t, "VNC connection opened (unencrypted)"); !e.OK {
		t.Fatalf("%+v", e)
	}
	// the viewer hangs up: the page is told, and the bridge is closed
	fb.onEnd(nil)
	if r := h.rec.wait(t, "vnc:closed:"+info.SID).data.(map[string]any)["reason"]; r != "ended" {
		t.Fatalf("%v", r)
	}
	if !fb.isClosed() {
		t.Fatal("the bridge was not closed")
	}
}

func TestVNCThroughSSHReachesTheServerOnTheHostItself(t *testing.T) {
	h := newHarness(t).unlocked(t)
	fb := installVNC(h)
	port := vncEcho(t)
	srv := startServer(t) // the test SSH server forwards to the loopback of this machine
	sshHost := h.sshHost(t, "via", srv, "")
	sshHost.VNCEnabled, sshHost.VNCViaSSH, sshHost.VNCPort = true, true, port
	saved, err := h.app.Host.SaveHost(sshHost)
	if err != nil {
		t.Fatal(err)
	}
	type res struct {
		info VncInfo
		err  error
	}
	done := make(chan res, 1)
	go func() { i, err := h.app.Vnc.Open(saved.ID, "vnc-key-1"); done <- res{i, err} }()
	h.trust(t, "vnc-key-1") // the same host key question as any SSH connection
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	c, err := fb.dial()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := readN(t, c, 3); got != "RFB" {
		t.Fatalf("through the tunnel: %q", got)
	}
	if e := h.waitLog(t, "VNC connection opened through SSH"); !e.OK {
		t.Fatalf("%+v", e)
	}
	if err := h.app.Vnc.Close(r.info.SID); err != nil || !fb.isClosed() {
		t.Fatalf("Close: %v closed=%v", err, fb.isClosed())
	}
	if err := h.app.Vnc.Close(r.info.SID); err != nil {
		t.Fatal("closing twice is not an error")
	}
}

func TestVNCRefuses(t *testing.T) {
	h := newHarness(t).unlocked(t)
	installVNC(h)
	off, _ := h.app.Host.SaveHost(domain.Host{Label: "a", Hostname: "a"})
	if _, err := h.app.Vnc.Open(off.ID, ""); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("%v", err)
	}
	jump, _ := h.app.Host.SaveHost(domain.Host{Label: "b", Hostname: "b", VNCEnabled: true, JumpHostID: off.ID})
	if _, err := h.app.Vnc.Open(jump.ID, ""); err == nil || !strings.Contains(err.Error(), "SSH login") {
		t.Fatalf("a direct VNC through a jump host: %v", err)
	}
	ssh, _ := h.app.Host.SaveHost(domain.Host{Label: "c", Hostname: "c", VNCEnabled: true, VNCViaSSH: true})
	if _, err := h.app.Vnc.Open(ssh.ID, ""); err == nil || !strings.Contains(err.Error(), "prompt key") {
		t.Fatalf("%v", err)
	}
	if _, err := h.app.Vnc.Open("nope", ""); err == nil {
		t.Fatal("no such host")
	}
	h.core.StartVNC = func(func() (net.Conn, error), func(error)) (VNCBridge, error) { return nil, errors.New("no free port") }
	on, _ := h.app.Host.SaveHost(domain.Host{Label: "d", Hostname: "127.0.0.1", VNCEnabled: true})
	if _, err := h.app.Vnc.Open(on.ID, ""); err == nil || !strings.Contains(err.Error(), "no free port") {
		t.Fatalf("%v", err)
	}
	h.core.StartVNC = nil
	if _, err := h.app.Vnc.Open(on.ID, ""); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("%v", err)
	}
	for _, bad := range []domain.Host{{Label: "e", Hostname: "e", VNCEnabled: true, VNCPort: 70000}} {
		if _, err := h.app.Host.SaveHost(bad); err == nil {
			t.Fatalf("%+v was accepted", bad)
		}
	}
	d, _ := h.app.Host.SaveHost(domain.Host{Label: "f", Hostname: "f", VNCEnabled: true})
	if d.VNCPort != 5900 {
		t.Fatalf("the default port: %d", d.VNCPort)
	}
}
