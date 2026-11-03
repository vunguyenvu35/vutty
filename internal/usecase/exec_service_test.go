package usecase

import (
	"strings"
	"testing"
	"time"

	"vutty/internal/domain"
	"vutty/internal/testssh"
)

func execServer(t *testing.T, f func(cmd string) (string, string, int)) *testssh.Server {
	t.Helper()
	srv, err := testssh.Start(testssh.Config{User: "root", Password: "pw", ExecFunc: f})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv
}

// trusted makes a host whose key is already trusted, the way a host that was opened once in a terminal tab is.
func (h *harness) trusted(t *testing.T, label string, srv *testssh.Server) domain.Host {
	t.Helper()
	host := h.sshHost(t, label, srv, "")
	sid, err := h.open(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	h.trust(t, sid)
	h.rec.waitOutput(t, sid, "READY")
	if err := h.app.Session.Close(sid); err != nil {
		t.Fatal(err)
	}
	return host
}

func (h *harness) finished(t *testing.T, id string) ExecRun {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		r, err := h.app.Exec.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Done {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the run did not finish")
	return ExecRun{}
}

func TestRunOnHostsCollectsEachHostsOutputAndExitCode(t *testing.T) {
	h := newHarness(t).unlocked(t)
	echo := func(cmd string) (string, string, int) { return "ran: " + cmd + "\n", "", 0 }
	a := h.trusted(t, "web-1", execServer(t, echo))
	b := h.trusted(t, "web 2", execServer(t, echo))
	c := h.trusted(t, "db", execServer(t, func(string) (string, string, int) { return "partial\n", "boom\n", 2 }))

	id, err := h.app.Exec.Run(ExecSpec{HostIDs: []string{a.ID, b.ID, c.ID}, Command: "uptime {{host}}"})
	if err != nil {
		t.Fatal(err)
	}
	r := h.finished(t, id)
	if len(r.Results) != 3 {
		t.Fatalf("%+v", r)
	}
	// the order of the hosts is kept, and each one's own name went into its command
	if r.Results[0].State != "ok" || r.Results[0].Output != "ran: uptime web-1\n" {
		t.Fatalf("%+v", r.Results[0])
	}
	if r.Results[1].Output != "ran: uptime 'web 2'\n" { // a label with a space is quoted
		t.Fatalf("%q", r.Results[1].Output)
	}
	f := r.Results[2]
	if f.State != "failed" || f.ExitCode != 2 || !strings.Contains(f.Output, "partial") || !strings.Contains(f.Output, "boom") || !strings.Contains(f.Error, "status 2") {
		t.Fatalf("a failing command: %+v", f)
	}
	if r.Results[0].DurationMS < 0 || r.Results[0].Label != "web-1" {
		t.Fatalf("%+v", r.Results[0])
	}
	// the last event sent to the screen is the finished run
	evs := h.rec.eventsNamed("exec:update:" + id)
	if len(evs) < 3 || !evs[len(evs)-1].data.(ExecRun).Done {
		t.Fatalf("%d events", len(evs))
	}
}

func TestRunOnHostsRefusesAHostWhoseKeyIsNotTrustedYet(t *testing.T) {
	h := newHarness(t).unlocked(t)
	host := h.sshHost(t, "new-server", execServer(t, func(string) (string, string, int) { return "x", "", 0 }), "")
	id, err := h.app.Exec.Run(ExecSpec{HostIDs: []string{host.ID}, Command: "id"})
	if err != nil {
		t.Fatal(err)
	}
	f := h.finished(t, id).Results[0]
	if f.State != "failed" || !strings.Contains(f.Error, "not trusted yet") || f.Output != "" {
		t.Fatalf("%+v", f)
	}
}

func TestRunOnHostsGivesUpOnASlowHostAfterTheTimeout(t *testing.T) {
	h := newHarness(t).unlocked(t)
	slow := execServer(t, func(string) (string, string, int) { time.Sleep(4 * time.Second); return "late", "", 0 })
	host := h.trusted(t, "slow", slow)
	start := time.Now()
	id, _ := h.app.Exec.Run(ExecSpec{HostIDs: []string{host.ID}, Command: "sleep 100", TimeoutSec: 1})
	f := h.finished(t, id).Results[0]
	if f.State != "failed" || !strings.Contains(f.Error, "timed out after 1 seconds") {
		t.Fatalf("%+v", f)
	}
	if time.Since(start) > 3500*time.Millisecond {
		t.Fatal("it waited for the command instead of the timeout")
	}
}

func TestCancelStopsTheRunningHostsAndTheOnesNotStarted(t *testing.T) {
	h := newHarness(t).unlocked(t)
	slow := func(string) (string, string, int) { time.Sleep(5 * time.Second); return "", "", 0 }
	var ids []string
	for i := 0; i < execConcurrency+2; i++ {
		ids = append(ids, h.trusted(t, "h"+string(rune('a'+i)), execServer(t, slow)).ID)
	}
	id, _ := h.app.Exec.Run(ExecSpec{HostIDs: ids, Command: "sleep 5"})
	time.Sleep(400 * time.Millisecond) // five are running, two wait
	if err := h.app.Exec.Cancel(id); err != nil {
		t.Fatal(err)
	}
	r := h.finished(t, id)
	for _, x := range r.Results {
		if x.State != "canceled" {
			t.Fatalf("%s is %s: %+v", x.Label, x.State, x)
		}
	}
}

func TestOutputIsCappedAndSaysSo(t *testing.T) {
	h := newHarness(t).unlocked(t)
	big := strings.Repeat("0123456789", 20000) // 200 KB
	host := h.trusted(t, "noisy", execServer(t, func(string) (string, string, int) { return big, "", 0 }))
	id, _ := h.app.Exec.Run(ExecSpec{HostIDs: []string{host.ID}, Command: "cat big"})
	f := h.finished(t, id).Results[0]
	if f.State != "ok" || !f.Truncated || len(f.Output) != execOutputCap {
		t.Fatalf("%s truncated=%v len=%d", f.State, f.Truncated, len(f.Output))
	}
}

func TestRunValidatesWhatItIsGiven(t *testing.T) {
	h := newHarness(t).unlocked(t)
	host := h.sshHost(t, "x", execServer(t, nil), "")
	for name, spec := range map[string]ExecSpec{
		"no command":   {HostIDs: []string{host.ID}, Command: "  \n"},
		"no host":      {Command: "id"},
		"unknown host": {HostIDs: []string{"nope"}, Command: "id"},
		"NUL":          {HostIDs: []string{host.ID}, Command: "id\x00"},
		"too long":     {HostIDs: []string{host.ID}, Command: strings.Repeat("a", execMaxCommand+1)},
	} {
		if _, err := h.app.Exec.Run(spec); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := h.app.Exec.Get("nope"); err == nil {
		t.Error("an unknown run was found")
	}
	h.app.Vault.Lock()
	if _, err := h.app.Exec.Run(ExecSpec{HostIDs: []string{host.ID}, Command: "id"}); err == nil {
		t.Error("a locked vault ran a command")
	}
}

func TestOnlyTheLastRunsAreKeptAndAForgottenRunIsGone(t *testing.T) {
	h := newHarness(t).unlocked(t)
	host := h.trusted(t, "h", execServer(t, func(string) (string, string, int) { return "x", "", 0 }))
	var first string
	for i := 0; i < execKeepRuns+2; i++ {
		id, err := h.app.Exec.Run(ExecSpec{HostIDs: []string{host.ID}, Command: "id"})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = id
		}
		h.finished(t, id)
	}
	if _, err := h.app.Exec.Get(first); err == nil {
		t.Fatal("the oldest run was kept")
	}
	id, _ := h.app.Exec.Run(ExecSpec{HostIDs: []string{host.ID}, Command: "id"})
	h.finished(t, id)
	if err := h.app.Exec.Forget(id); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Exec.Get(id); err == nil {
		t.Fatal("a forgotten run is still there")
	}
}

func TestExpandHostVars(t *testing.T) {
	h := domain.Host{Label: "web-1", Hostname: "10.0.0.5", Username: "deploy"}
	for in, want := range map[string]string{
		"echo {{host}} {{ hostname }} {{user}}": "echo web-1 10.0.0.5 deploy",
		"{{other}} stays":                       "{{other}} stays",
		"no vars":                               "no vars",
	} {
		if got := expandHostVars(in, h); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	odd := domain.Host{Label: "it's; rm -rf /", Hostname: "h", Username: "$(id)"}
	if got := expandHostVars("{{host}} {{user}}", odd); got != `'it'\''s; rm -rf /' '$(id)'` {
		t.Errorf("a value with shell characters must be quoted: %q", got)
	}
}

// eventsNamed returns the events recorded under name, in order.
func (r *recorder) eventsNamed(name string) []event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []event
	for _, e := range r.events {
		if e.name == name {
			out = append(out, e)
		}
	}
	return out
}
