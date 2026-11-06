package app

import (
	"errors"
	"strings"
	"testing"

	"vutty/internal/usecase"
)

func TestRDPCommandPerSystem(t *testing.T) {
	spec := usecase.RDPSpec{Host: "win.example.com", Port: 3389, User: "ann", File: "/tmp/h.rdp"}
	none := func(string) (string, error) { return "", errors.New("not found") }
	if c, err := rdpCommand("windows", spec, none); err != nil || strings.Join(c.Args, " ") != "mstsc /tmp/h.rdp" {
		t.Fatalf("%v %v", c, err)
	}
	if c, err := rdpCommand("darwin", spec, none); err != nil || strings.Join(c.Args, " ") != "open /tmp/h.rdp" {
		t.Fatalf("%v %v", c, err)
	}
	if _, err := rdpCommand("linux", spec, none); err == nil || !strings.Contains(err.Error(), "FreeRDP") {
		t.Fatalf("no client: %v", err)
	}
	only := func(name string) func(string) (string, error) {
		return func(b string) (string, error) {
			if b == name {
				return "/usr/bin/" + b, nil
			}
			return "", errors.New("not found")
		}
	}
	c, err := rdpCommand("linux", spec, only("xfreerdp"))
	if err != nil || c.Args[0] != "/usr/bin/xfreerdp" || strings.Join(c.Args[1:], " ") != "/v:win.example.com:3389 /cert:tofu /dynamic-resolution +clipboard /u:ann" {
		t.Fatalf("%v %v", c, err)
	}
	// the user name is one argument, whatever it holds
	spec.User = "a b /v:evil"
	c, _ = rdpCommand("linux", spec, only("xfreerdp3"))
	if c.Args[len(c.Args)-1] != "/u:a b /v:evil" {
		t.Fatalf("%v", c.Args)
	}
	if c, err := rdpCommand("linux", spec, only("remmina")); err != nil || strings.Join(c.Args, " ") != "/usr/bin/remmina -c /tmp/h.rdp" {
		t.Fatalf("%v %v", c, err)
	}
}
