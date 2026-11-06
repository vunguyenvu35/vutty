package usecase

import (
	"os"
	"strings"
	"testing"

	"vutty/internal/domain"
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
