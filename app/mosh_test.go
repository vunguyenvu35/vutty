package app

import (
	"errors"
	"strings"
	"testing"

	"vutty/internal/usecase"
)

func TestFindMoshClientTriesThePathThenTheUsualFolders(t *testing.T) {
	none := func(string) (string, error) { return "", errors.New("no") }
	if p, err := findMoshClient(func(string) (string, error) { return "/x/mosh-client", nil }, func(string) bool { return false }); err != nil || p != "/x/mosh-client" {
		t.Fatalf("%q %v", p, err)
	}
	if p, err := findMoshClient(none, func(p string) bool { return p == "/opt/homebrew/bin/mosh-client" }); err != nil || p != "/opt/homebrew/bin/mosh-client" {
		t.Fatalf("a Mac with Homebrew, started from the Dock: %q %v", p, err)
	}
	if _, err := findMoshClient(none, func(string) bool { return false }); err == nil || !strings.Contains(err.Error(), "mosh") {
		t.Fatalf("%v", err)
	}
}

func TestTheKeyIsInTheEnvironmentNotTheArguments(t *testing.T) {
	c := moshClientCommand("/usr/bin/mosh-client", usecase.MoshTarget{IP: "203.0.113.7", Port: 60001, Key: "AbCdEfGhIjKlMnOpQrStUv"})
	if strings.Join(c.Args, " ") != "/usr/bin/mosh-client 203.0.113.7 60001" {
		t.Fatalf("%v", c.Args)
	}
	if len(c.Env) != 1 || c.Env[0] != "MOSH_KEY=AbCdEfGhIjKlMnOpQrStUv" {
		t.Fatalf("%v", c.Env)
	}
}
