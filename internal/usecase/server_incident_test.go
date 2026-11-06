package usecase

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"vutty/internal/testssh"
)

func TestLogQueryTimesAreCheckedAndWrittenByUs(t *testing.T) {
	q, err := logQueryOf(300, "", "2026-11-06T03:12:00+07:00", "2026-11-06T03:20:00+07:00", false)
	if err != nil {
		t.Fatal(err)
	}
	if q.jSince != ` --since "2026-11-05 20:12:00 UTC" --until "2026-11-05 20:20:00 UTC"` || q.dSince != " --since 2026-11-05T20:12:00Z --until 2026-11-05T20:20:00Z" || !q.ranged {
		t.Fatalf("%+v", q)
	}
	for name, c := range map[string][3]string{
		"injection":  {"2026-11-06T03:12:00Z; id", "", ""},
		"not a time": {"yesterday", "", ""},
		"backwards":  {"2026-11-06T04:00:00Z", "2026-11-06T03:00:00Z", ""},
		"quote":      {`2026-11-06T03:00:00Z" --output=x`, "", ""},
	} {
		if _, err := logQueryOf(300, "", c[0], c[1], false); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := logQueryOf(300, "", "2026-11-06T03:00:00Z", "", true); err == nil {
		t.Error("a boot and a range together")
	}
	if _, err := logQueryOf(7, "", "2026-11-06T03:00:00Z", "", false); err == nil {
		t.Error("another number of lines")
	}
}

func TestLogCommandForARangeBootAndFollow(t *testing.T) {
	q, _ := logQueryOf(1000, "", "2026-11-06T03:00:00Z", "2026-11-06T03:30:00Z", false)
	c, err := logCommand("service", "nginx", q)
	if err != nil || c != `journalctl -u nginx.service -n 1000 --since "2026-11-06 03:00:00 UTC" --until "2026-11-06 03:30:00 UTC" --no-pager -o short-iso 2>&1` {
		t.Fatalf("%q %v", c, err)
	}
	c, err = logCommand("container", "web", q)
	if err != nil || !strings.Contains(c, "docker logs --tail 1000 --since 2026-11-06T03:00:00Z --until 2026-11-06T03:30:00Z --timestamps web") {
		t.Fatalf("%q %v", c, err)
	}
	if _, err := logCommand("web", "nginx", q); err == nil || !strings.Contains(err.Error(), "no times") {
		t.Fatalf("the nginx error file has no times: %v", err)
	}
	b, _ := logQueryOf(300, "", "", "", true)
	if c, err := logCommand("", "", b); err != nil || c != "journalctl -n 300 -b -1 --no-pager -o short-iso 2>&1" {
		t.Fatalf("%q %v", c, err)
	}
	if _, err := logCommand("container", "web", b); err == nil {
		t.Fatal("docker has no boots")
	}
	f, _ := logQueryOf(300, "15m", "", "", false)
	f.follow = true
	if c, _ := logCommand("service", "sshd", f); c != `journalctl -u sshd.service -f -n 300 --since "15 min ago" --no-pager -o short-iso 2>&1` {
		t.Fatalf("%q", c)
	}
	if c, _ := logCommand("web", "nginx", f); !strings.Contains(c, "tail -n 300 -F /var/log/nginx/error.log") {
		t.Fatalf("%q", c)
	}
}

func TestParseClock(t *testing.T) {
	c, err := parseClock("2026-11-06T03:12:44+0700|+07\n")
	if err != nil || c.Offset != 7*3600 || c.Zone != "+07" || c.Now != "2026-11-06T03:12:44+07:00" {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := parseClock("garbage"); err == nil {
		t.Fatal("garbage was read")
	}
}

const incidentSample = `##access
##persistent
yes
##oldest
2026-11-01T00:00:03+0000 host kernel: Linux version 6.8
##events
2026-11-06T03:10:01+0000 host systemd[1]: app.service: Scheduled restart job, restart counter is at 3.
2026-11-06T03:09:58+0000 host kernel: Out of memory: Killed process 1234 (node)
2026-11-06T03:09:59+0000 host systemd[1]: app.service: Failed with result 'oom-kill'.
2026-11-06T03:11:00+0000 host systemd-logind[600]: System is rebooting.
2026-11-06T03:12:00+0000 host kernel: Linux version 6.8.0
##errors
      3 2026-11-06T03:09+0000
     12 2026-11-06T03:10+0000
not a bucket
##docker
present
/web|2|false|0|true|2026-11-06T03:10:05.123456789Z|0001-01-01T00:00:00Z
/db|0|true|137|false|2026-11-01T00:00:00Z|2026-11-06T03:09:58.5Z
/old|5|false|0|true|2026-10-01T00:00:00Z|0001-01-01T00:00:00Z
`

func TestParseIncident(t *testing.T) {
	from, _ := time.Parse(time.RFC3339, "2026-11-06T03:00:00Z")
	inc := parseIncident(incidentSample, from, from.Add(time.Hour))
	if !inc.Persistent || inc.Limited || !inc.HasDocker || inc.Oldest != "2026-11-01T00:00:03Z" {
		t.Fatalf("%+v", inc)
	}
	var kinds []string
	for _, e := range inc.Events {
		kinds = append(kinds, e.Kind)
	}
	if got := strings.Join(kinds, ","); got != "oom,oom,restart,shutdown,boot" { // by time
		t.Fatalf("%s", got)
	}
	if len(inc.Errors) != 2 || inc.Errors[1].Count != 12 || inc.Errors[1].Minute != "2026-11-06T03:10:00Z" {
		t.Fatalf("%+v", inc.Errors)
	}
	// "old" ran before the range and was not touched in it: left out, though its restart count is high
	if len(inc.Containers) != 2 || inc.Containers[0].Name != "web" || inc.Containers[1].Name != "db" || !inc.Containers[1].OOM || inc.Containers[1].ExitCode != 137 {
		t.Fatalf("%+v", inc.Containers)
	}
}

func TestIncidentRefusesBadRanges(t *testing.T) {
	h := newHarness(t).unlocked(t)
	for name, c := range map[string][2]string{
		"injection": {"2026-11-06T03:00:00Z'; id; '", "2026-11-06T04:00:00Z"},
		"backwards": {"2026-11-06T04:00:00Z", "2026-11-06T03:00:00Z"},
		"too long":  {"2026-11-01T00:00:00Z", "2026-11-06T00:00:00Z"},
	} {
		if _, err := h.app.Server.Incident("x", c[0], c[1]); err == nil || strings.Contains(err.Error(), "connection to the server is closed") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A followed log end to end on the test SSH server: lines come in batches until the screen stops it, and the host command is told
// to stop (its input ends).
func TestLogsFollowStreamsAndStops(t *testing.T) {
	h := newHarness(t).unlocked(t)
	var mu sync.Mutex
	var sent string
	ended := make(chan struct{}, 1)
	srv, err := testssh.Start(testssh.Config{User: "root", Password: "pw",
		ExecFunc:    func(cmd string) (string, string, int) { return "", "", 0 },
		StreamMatch: func(cmd string) bool { return strings.Contains(cmd, "journalctl") },
		ExecStream: func(cmd string, stdin io.Reader, stdout, stderr io.Writer) int {
			mu.Lock()
			sent = cmd
			mu.Unlock()
			_, _ = io.WriteString(stdout, "2026-11-06T03:00:00+0000 h a: one\r\n2026-11-06T03:00:01+0000 h a: two\n2026-11-06T03:00:02+0000 h a: thr")
			_, _ = io.Copy(io.Discard, stdin)
			ended <- struct{}{}
			return 0
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	sid := h.openServer(t, srv)

	if err := h.app.Server.LogsFollowStart(sid, "bad key!", "", "", 300, ""); err == nil {
		t.Fatal("a key with a space was accepted")
	}
	if err := h.app.Server.LogsFollowStart(sid, "follow-key-1", "service", "nginx; id", 300, ""); err == nil {
		t.Fatal("a name with a command was accepted")
	}
	if err := h.app.Server.LogsFollowStart(sid, "follow-key-1", "service", "sshd", 300, ""); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Server.LogsFollowStart(sid, "follow-key-1", "service", "sshd", 300, ""); err == nil {
		t.Fatal("the same key twice")
	}
	ev := h.rec.wait(t, "server:log:follow-key-1")
	lines := ev.data.(map[string]any)["lines"].([]string)
	if len(lines) != 2 || lines[0] != "2026-11-06T03:00:00+0000 h a: one" || strings.Contains(lines[0], "\r") {
		t.Fatalf("%q", lines)
	}
	mu.Lock()
	if !strings.Contains(sent, "journalctl -u sshd.service -f -n 300") || !strings.Contains(sent, "set -m") {
		t.Fatalf("%q", sent)
	}
	mu.Unlock()
	if err := h.app.Server.LogsFollowStop(sid, "follow-key-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the host command was not told to stop")
	}
	h.rec.wait(t, "server:logend:follow-key-1")
	if err := h.app.Server.LogsFollowStop(sid, "follow-key-1"); err != nil {
		t.Fatal("stopping twice is not an error")
	}
}
