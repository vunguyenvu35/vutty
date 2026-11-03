package usecase

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"vutty/internal/domain"
	"vutty/internal/testssh"
)

func (h *harness) keyHost(t *testing.T, srvCfg testssh.Config, forward bool) (domain.Host, *testssh.Server, domain.Identity) {
	t.Helper()
	id, err := h.app.Vault.GenerateKey("k", "ed25519")
	if err != nil {
		t.Fatal(err)
	}
	pk, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(id.PublicKey))
	srvCfg.User, srvCfg.AuthKeys = "root", []ssh.PublicKey{pk}
	srv, err := testssh.Start(srvCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	m := modelHost("fwd", srv.Port(), id.ID)
	m.ForwardAgent = forward
	host, err := h.app.Host.SaveHost(m)
	if err != nil {
		t.Fatal(err)
	}
	return host, srv, id
}

func fingerprintOf(t *testing.T, line string) string {
	t.Helper()
	pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	return ssh.FingerprintSHA256(pk)
}

func waitKeys(t *testing.T, srv *testssh.Server, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if k := srv.AgentKeys(); len(k) >= n {
			return k
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the server never saw %d forwarded key(s): %v", n, srv.AgentKeys())
	return nil
}

func TestForwardAgentOffersNothingByDefault(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	h := newHarness(t).unlocked(t)
	host, srv, _ := h.keyHost(t, testssh.Config{}, false)
	sid, _ := h.open(host.ID, 80, 24)
	h.trust(t, sid)
	h.rec.waitOutput(t, sid, "READY")
	time.Sleep(200 * time.Millisecond)
	if srv.AgentRequests() != 0 {
		t.Fatalf("the server was asked to forward an agent although the host did not ask for it")
	}
}

func TestForwardAgentOffersTheKeyOfTheHostWhenThereIsNoAgent(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	h := newHarness(t).unlocked(t)
	host, srv, id := h.keyHost(t, testssh.Config{}, true)
	sid, _ := h.open(host.ID, 80, 24)
	h.trust(t, sid)
	h.rec.waitOutput(t, sid, "READY")
	keys := waitKeys(t, srv, 1)
	if len(keys) != 1 || keys[0] != fingerprintOf(t, id.PublicKey) {
		t.Fatalf("the server saw %v, want only the key of the host's identity", keys)
	}
	if srv.AgentRequests() != 1 {
		t.Fatalf("%d requests", srv.AgentRequests())
	}
}

func TestForwardAgentUsesTheRunningSSHAgentWhenThereIsOne(t *testing.T) {
	// an ssh-agent with a key of its own, on a socket (short path: unix sockets have a small limit)
	dir, err := os.MkdirTemp("/tmp", "vt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "agent.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("no unix sockets here:", err)
	}
	t.Cleanup(func() { ln.Close() })
	ring := agent.NewKeyring()
	other := newHarness(t).unlocked(t)
	oid, _ := other.app.Vault.GenerateKey("agent-key", "ed25519")
	pem, _ := other.core.Vault.GetSecret(oid.SecretRef)
	raw, _ := ssh.ParseRawPrivateKey(pem)
	if err := ring.Add(agent.AddedKey{PrivateKey: normalizeKey(raw), Comment: "from the agent"}); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go agent.ServeAgent(ring, c)
		}
	}()
	t.Setenv("SSH_AUTH_SOCK", sock)

	h := newHarness(t).unlocked(t)
	host, srv, mine := h.keyHost(t, testssh.Config{}, true)
	sid, _ := h.open(host.ID, 80, 24)
	h.trust(t, sid)
	h.rec.waitOutput(t, sid, "READY")
	keys := waitKeys(t, srv, 1)
	if len(keys) != 1 || keys[0] != fingerprintOf(t, oid.PublicKey) {
		t.Fatalf("the server saw %v, want the key of the running agent", keys)
	}
	if keys[0] == fingerprintOf(t, mine.PublicKey) {
		t.Fatal("the vault key was offered although an agent is running")
	}
}

func TestForwardAgentThatCannotBeSetUpStillOpensTheTerminalAndSaysWhy(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	h := newHarness(t).unlocked(t)
	// a password identity and no agent: nothing to offer
	srv := startServer(t)
	host := h.sshHost(t, "pw-host", srv, "")
	host.ForwardAgent = true
	if _, err := h.app.Host.SaveHost(host); err != nil {
		t.Fatal(err)
	}
	sid, _ := h.open(host.ID, 80, 24)
	h.trust(t, sid)
	h.rec.waitOutput(t, sid, "READY") // the terminal opened
	logs, _ := h.app.Log.List(domain.ActivityFilter{})
	found := false
	for _, l := range logs {
		if !l.OK && l.HostLabel == "pw-host" && strings.Contains(l.Message, "Agent forwarding is on for this host but could not be set up") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the Logs do not say why: %+v", logs)
	}
}

func TestForwardAgentRefusedByTheServerStillOpensTheTerminal(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	h := newHarness(t).unlocked(t)
	host, srv, _ := h.keyHost(t, testssh.Config{RefuseAgent: true}, true)
	sid, _ := h.open(host.ID, 80, 24)
	h.trust(t, sid)
	h.rec.waitOutput(t, sid, "READY")
	if srv.AgentRequests() != 1 || len(srv.AgentKeys()) != 0 {
		t.Fatalf("requests %d keys %v", srv.AgentRequests(), srv.AgentKeys())
	}
}
