package app

import (
	"strings"
	"testing"
)

func TestParseSSHLink(t *testing.T) {
	for raw, want := range map[string]SSHLink{
		"ssh://example.com":                    {Hostname: "example.com", Port: 22},
		"ssh://deploy@web-1.example.com":       {Username: "deploy", Hostname: "web-1.example.com", Port: 22},
		"ssh://root@10.0.0.5:2222":             {Username: "root", Hostname: "10.0.0.5", Port: 2222},
		"SSH://Admin@Host.Example:22/":         {Username: "Admin", Hostname: "Host.Example", Port: 22},
		"ssh://u@[2001:db8::1]:2200":           {Username: "u", Hostname: "2001:db8::1", Port: 2200},
		"ssh://user:secret@host.example/path":  {Username: "user", Hostname: "host.example", Port: 22}, // the password is dropped
		"ssh://host.example/?cmd=rm%20-rf%20/": {Hostname: "host.example", Port: 22},                   // so are the path and the query
		"  ssh://trim.example  ":               {Hostname: "trim.example", Port: 22},
		"ssh://under_score.example":            {Hostname: "under_score.example", Port: 22},
	} {
		got, err := ParseSSHLink(raw)
		if err != nil || got != want {
			t.Errorf("%q: got %+v %v, want %+v", raw, got, err, want)
		}
	}
}

func TestParseSSHLinkRefusesWhatIsNotAPlainSSHLink(t *testing.T) {
	for _, raw := range []string{
		"", "ssh://", "ssh:///path", "http://example.com", "https://example.com", "file:///etc/passwd", "javascript:alert(1)", "example.com", "ssh:example.com",
		"ssh://-oProxyCommand=evil", "ssh://-host.example", "ssh://user@-host", "ssh://-u@host.example",
		"ssh://host.example:0", "ssh://host.example:65536", "ssh://host.example:abc",
		"ssh://ho st.example", "ssh://host.example\nssh://other.example", "ssh://host\x00.example", "ssh://host;rm -rf /", "ssh://$(id).example", "ssh://`id`.example",
		"ssh://us er@host.example", "ssh://u;s@host.example", "ssh://u$@host.example",
		"ssh://" + strings.Repeat("a", 254), "ssh://" + strings.Repeat("u", 65) + "@host.example", "ssh://host.example/" + strings.Repeat("a", 3000),
	} {
		if got, err := ParseSSHLink(raw); err == nil {
			t.Errorf("%q was accepted as %+v", raw, got)
		}
	}
}

func TestALinkWaitsUntilTheScreenTakesItOnce(t *testing.T) {
	s := NewSystemService() // no window yet: the link is kept
	if l := s.TakePendingSSHLink(); l.Hostname != "" {
		t.Fatalf("a link out of nothing: %+v", l)
	}
	s.OpenSSHLink("ssh://deploy@web-1.example.com:2222")
	l := s.TakePendingSSHLink()
	if l != (SSHLink{Username: "deploy", Hostname: "web-1.example.com", Port: 2222}) {
		t.Fatalf("%+v", l)
	}
	if again := s.TakePendingSSHLink(); again.Hostname != "" {
		t.Fatalf("a link that was taken came back: %+v", again)
	}
	s.OpenSSHLink("ssh://first.example")
	s.OpenSSHLink("ssh://second.example") // the newest one counts
	if l := s.TakePendingSSHLink(); l.Hostname != "second.example" {
		t.Fatalf("%+v", l)
	}
	s.OpenSSHLink("not a link")
	s.OpenSSHLink("ssh://-oProxyCommand=x")
	if l := s.TakePendingSSHLink(); l.Hostname != "" {
		t.Fatalf("an invalid link was kept: %+v", l)
	}
}
