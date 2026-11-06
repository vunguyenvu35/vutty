package usecase

import (
	"os/exec"
	"strings"
	"testing"
)

func TestParseUpgrade(t *testing.T) {
	if u := parseUpgrade("##none\n"); u.Started || u.Running || u.Done {
		t.Fatalf("%+v", u)
	}
	run := parseUpgrade("##running\n##log\nReading package lists...\nUnpacking x\n")
	if !run.Started || !run.Running || run.Done || run.Log != "Reading package lists...\nUnpacking x" {
		t.Fatalf("%+v", run)
	}
	ok := parseUpgrade("##rc 0\n##log\ndone\n")
	if !ok.Done || !ok.OK || ok.Code != 0 || ok.Running {
		t.Fatalf("%+v", ok)
	}
	bad := parseUpgrade("##rc 100\n##log\nE: could not get lock\n")
	if !bad.Done || bad.OK || bad.Code != 100 || !strings.Contains(bad.Log, "lock") {
		t.Fatalf("%+v", bad)
	}
	// a stale rc file with a process that runs again: still running
	if r := parseUpgrade("##running\n##rc 0\n##log\nx"); r.Done || !r.Running {
		t.Fatalf("%+v", r)
	}
}

func TestUpgradeScriptsAreValidShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	for name, script := range map[string]string{"start": upgradeStartScript(), "status": upgradeStatusScript} {
		if out, err := exec.Command(sh, "-n", "-c", script).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
	}
}
