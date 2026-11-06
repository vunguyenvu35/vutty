package usecase

import (
	"errors"
	"os"
	"strings"
	"testing"

	"vutty/internal/domain"
	"vutty/internal/testssh"
)

func TestRDPFileHasTheAddressAndUserAndNoPassword(t *testing.T) {
	got, err := rdpFile("win.example.com", 3390, "DOM\\ann")
	if err != nil {
		t.Fatal(err)
	}
	want := "full address:s:win.example.com:3390\r\nusername:s:DOM\\ann\r\nprompt for credentials:i:1\r\nauthentication level:i:2\r\n"
	if got != want || strings.Contains(strings.ToLower(got), "password") {
		t.Fatalf("%q", got)
	}
	if got, _ := rdpFile("h", 3389, ""); strings.Contains(got, "username") {
		t.Fatalf("no user, no user line: %q", got)
	}
	for _, bad := range [][2]string{{"h\r\nscreen mode id:i:1", ""}, {"h", "u\nx"}} {
		if _, err := rdpFile(bad[0], 3389, bad[1]); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestOpenRDPWritesTheFileAndStartsTheClient(t *testing.T) {
	h := newHarness(t).unlocked(t)
	var got RDPSpec
	var text string
	h.core.OpenRDP = func(s RDPSpec) error {
		got = s
		b, err := os.ReadFile(s.File)
		text = string(b)
		return err
	}
	h.core.CacheDir = t.TempDir()
	host, err := h.app.Host.SaveHost(domain.Host{Label: "win", Hostname: "win.example.com", Username: "ann", RDPEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if host.RDPPort != 3389 {
		t.Fatalf("the default port: %d", host.RDPPort)
	}
	if err := h.app.Host.OpenRDP(host.ID); err != nil {
		t.Fatal(err)
	}
	if got.Host != "win.example.com" || got.Port != 3389 || got.User != "ann" || !strings.HasPrefix(text, "full address:s:win.example.com:3389") {
		t.Fatalf("%+v %q", got, text)
	}
	if st, err := os.Stat(got.File); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the file is private: %v %v", st, err)
	}
	other, _ := h.app.Host.SaveHost(domain.Host{Label: "win2", Hostname: "w2", Username: "ann", RDPEnabled: true, RDPUsername: "admin", RDPPort: 4000})
	if err := h.app.Host.OpenRDP(other.ID); err != nil || got.User != "admin" || got.Port != 4000 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestOpenRDPRefuses(t *testing.T) {
	h := newHarness(t).unlocked(t)
	h.core.OpenRDP = func(RDPSpec) error { return nil }
	off, _ := h.app.Host.SaveHost(domain.Host{Label: "a", Hostname: "a"})
	if err := h.app.Host.OpenRDP(off.ID); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("%v", err)
	}
	jump, _ := h.app.Host.SaveHost(domain.Host{Label: "b", Hostname: "b", RDPEnabled: true, JumpHostID: off.ID})
	if err := h.app.Host.OpenRDP(jump.ID); err == nil || !strings.Contains(err.Error(), "jump host") {
		t.Fatalf("%v", err)
	}
	if err := h.app.Host.OpenRDP("nope"); err == nil {
		t.Fatal("no such host")
	}
	h.core.OpenRDP = nil
	on, _ := h.app.Host.SaveHost(domain.Host{Label: "c", Hostname: "c", RDPEnabled: true})
	if err := h.app.Host.OpenRDP(on.ID); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("%v", err)
	}
	if _, err := h.app.Host.SaveHost(domain.Host{Label: "d", Hostname: "d", RDPEnabled: true, RDPPort: 70000}); err == nil {
		t.Fatal("a port out of range")
	}
	if _, err := h.app.Host.SaveHost(domain.Host{Label: "e", Hostname: "e", RDPEnabled: true, RDPUsername: "a\nb"}); err == nil {
		t.Fatal("a user name with a line break")
	}
}

func TestParseMoshConnect(t *testing.T) {
	port, key, err := parseMoshConnect("\r\nMOSH CONNECT 60001 4NeCCgvZFe2RnPgrcU1PQw\r\n\r\nmosh-server (mosh 1.4.0)\r\n")
	if err != nil || port != 60001 || key != "4NeCCgvZFe2RnPgrcU1PQw" {
		t.Fatalf("%d %q %v", port, key, err)
	}
	for _, bad := range []string{"", "mosh-server is not installed on this host", "MOSH CONNECT 99999 4NeCCgvZFe2RnPgrcU1PQw", "MOSH CONNECT 60001 short", "xMOSH CONNECT 60001 4NeCCgvZFe2RnPgrcU1PQw"} {
		if _, _, err := parseMoshConnect(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if _, _, err := parseMoshConnect("mosh-server is not installed on this host"); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("the message of the host is the error: %v", err)
	}
	if baseHostID("mosh:h1") != "h1" || baseHostID("telnet:h2") != "h2" || baseHostID("h3") != "h3" {
		t.Fatal("baseHostID")
	}
	if ip, err := moshIP("203.0.113.7"); err != nil || ip != "203.0.113.7" {
		t.Fatalf("%q %v", ip, err)
	}
}

func moshServer(t *testing.T, reply string) (*testssh.Server, *string) {
	t.Helper()
	var sent string
	srv, err := testssh.Start(testssh.Config{User: "root", Password: "pw", ExecFunc: func(cmd string) (string, string, int) {
		sent = cmd
		return reply, "", 0
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv, &sent
}

func TestMoshSessionStartsTheServerOverSSHAndThenTheLocalClient(t *testing.T) {
	h := newHarness(t).unlocked(t)
	srv, sent := moshServer(t, "\r\nMOSH CONNECT 60007 4NeCCgvZFe2RnPgrcU1PQw\r\n")
	var got MoshTarget
	var fk deviceFakes
	h.core.OpenMosh = func(tg MoshTarget, cols, rows int, onData func([]byte)) (Shell, error) {
		got = tg
		s := newFakeShell(onData)
		fk.mu.Lock()
		fk.shells = append(fk.shells, s)
		fk.mu.Unlock()
		return s, nil
	}
	host := h.sshHost(t, "box", srv, "")
	sid, err := h.open(MoshID(host.ID), 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	h.trust(t, sid) // the same host key question as an SSH terminal
	if r := h.rec.wait(t, "term:ready:"+sid).data.(map[string]any)["host"]; r != "box" {
		t.Fatalf("%v", r)
	}
	if got != (MoshTarget{IP: "127.0.0.1", Port: 60007, Key: "4NeCCgvZFe2RnPgrcU1PQw"}) {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(*sent, "mosh-server new -s -c 256 -l LANG=en_US.UTF-8") || !strings.Contains(*sent, "C.UTF-8") {
		t.Fatalf("%q", *sent)
	}
	if e := h.waitLog(t, "Connected with Mosh to box"); !e.OK {
		t.Fatalf("%+v", e)
	}
	for _, e := range h.logs(t, domain.ActivityFilter{}) {
		if strings.Contains(e.Message, "4NeCCgvZFe2RnPgrcU1PQw") {
			t.Fatal("the key of the session must never reach the log")
		}
	}
	if err := h.app.Session.Write(sid, "ls\r"); err != nil {
		t.Fatal(err)
	}
	h.rec.waitOutput(t, sid, "echo:ls\r")
}

func TestMoshReportsWhatWentWrong(t *testing.T) {
	h := newHarness(t).unlocked(t)
	h.core.OpenMosh = func(MoshTarget, int, int, func([]byte)) (Shell, error) {
		return nil, errors.New("mosh-client was not found")
	}
	srv, _ := moshServer(t, "mosh-server is not installed on this host")
	host := h.sshHost(t, "box", srv, "")
	sid, _ := h.open(MoshID(host.ID), 80, 24)
	h.trust(t, sid)
	if m := h.rec.wait(t, "term:closed:"+sid).data.(map[string]any); !strings.Contains(m["reason"].(string), "not installed") {
		t.Fatalf("%+v", m)
	}
	srv2, _ := moshServer(t, "MOSH CONNECT 60007 4NeCCgvZFe2RnPgrcU1PQw")
	host2 := h.sshHost(t, "box2", srv2, "")
	sid2, _ := h.open(MoshID(host2.ID), 80, 24)
	h.trust(t, sid2)
	if m := h.rec.wait(t, "term:closed:"+sid2).data.(map[string]any); !strings.Contains(m["reason"].(string), "mosh-client was not found") {
		t.Fatalf("the error of the local client: %+v", m)
	}
	// a jump host is refused before anything connects
	jumped := h.sshHost(t, "far", srv, host.ID)
	sid3, _ := h.open(MoshID(jumped.ID), 80, 24)
	if m := h.rec.wait(t, "term:closed:"+sid3).data.(map[string]any); !strings.Contains(m["reason"].(string), "jump host") {
		t.Fatalf("%+v", m)
	}
}
