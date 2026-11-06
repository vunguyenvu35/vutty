package usecase

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ServerClock is the time on the server and how far it is from UTC: a time typed for "the server's time" needs it, and a
// log line carries only that.
type ServerClock struct {
	Now    string `json:"now"`    // RFC 3339
	Zone   string `json:"zone"`   // CET, UTC...
	Offset int    `json:"offset"` // seconds east of UTC
}

// Clock reads the time on the server.
func (s *ServerService) Clock(sid string) (ServerClock, error) {
	out, err := s.run(sid, `date '+%Y-%m-%dT%H:%M:%S%z|%Z'`)
	if err != nil {
		return ServerClock{}, err
	}
	return parseClock(out)
}

func parseClock(out string) (ServerClock, error) {
	v, zone, _ := strings.Cut(strings.TrimSpace(out), "|")
	at, err := time.Parse("2006-01-02T15:04:05-0700", v)
	if err != nil {
		return ServerClock{}, errors.New("could not read the time of the server")
	}
	_, off := at.Zone()
	return ServerClock{Now: at.Format(time.RFC3339), Zone: zone, Offset: off}, nil
}

// IncidentEvent is one thing the journal says happened: a service that failed or was restarted by systemd, a process the kernel
// killed for memory, the machine starting or stopping.
type IncidentEvent struct {
	Time string `json:"time"` // RFC 3339, UTC
	Kind string `json:"kind"` // failed, restart, oom, boot, shutdown
	Text string `json:"text"`
}

// IncidentBucket is how many error lines the journal has in one minute.
type IncidentBucket struct {
	Minute string `json:"minute"` // RFC 3339, UTC
	Count  int    `json:"count"`
}

// IncidentBox is a container that restarted, was killed for memory or started or stopped in the time range.
type IncidentBox struct {
	Name     string `json:"name"`
	Restarts int    `json:"restarts"`
	OOM      bool   `json:"oom"`
	ExitCode int    `json:"exit_code"`
	Running  bool   `json:"running"`
	Started  string `json:"started"`  // RFC 3339, UTC
	Finished string `json:"finished"` // empty while it runs
}

// ServerIncident is what the server itself wrote down about a time range.
type ServerIncident struct {
	Events     []IncidentEvent  `json:"events"`
	Errors     []IncidentBucket `json:"errors"`
	Containers []IncidentBox    `json:"containers"`
	Persistent bool             `json:"persistent"` // the journal is kept on disk: it survives a reboot
	Oldest     string           `json:"oldest"`     // the first moment the journal still has, RFC 3339
	Limited    bool             `json:"limited"`    // this login sees only its own messages
	HasDocker  bool             `json:"has_docker"`
	Truncated  bool             `json:"truncated"`
}

const (
	incidentMaxSpan   = 72 * time.Hour
	incidentMaxEvents = 300
)

// incidentScript reads, in one pass over the journal of the time range, only the lines that tell what happened; a second pass
// counts the error lines by minute. Nothing is changed on the server.
func incidentScript(from, until string) string {
	j := fmt.Sprintf("journalctl --no-pager -o short-iso --since %q --until %q", from, until)
	return fmt.Sprintf(`
echo '##access'; journalctl -n 0 2>&1 | grep -qi 'not seeing messages' && echo limited
echo '##persistent'; [ -d /var/log/journal ] && echo yes || echo no
echo '##oldest'; journalctl --no-pager -o short-iso 2>/dev/null | grep -m1 -E '^[0-9]{4}-'
echo '##events'
%[1]s 2>/dev/null | grep -iE 'Failed with result|entered failed state|Main process exited|Failed to start|Scheduled restart job|out of memory|oom-kill|oom_reaper|Killed process|kernel: Linux version|systemd-logind\[[0-9]+\]: (System is|The system will)' | grep -v 'status=0/SUCCESS' | tail -n %[2]d
echo '##errors'
%[1]s -p err 2>/dev/null | grep -E '^[0-9]{4}-' | sed -E 's/^([0-9T:-]{16}):[0-9]{2}(.{5}).*/\1\2/' | sort | uniq -c
echo '##docker'
if command -v docker >/dev/null 2>&1; then
D=docker; docker info >/dev/null 2>&1 || D="sudo -n docker"
echo present
IDS=$($D ps -aq 2>/dev/null | head -n 100)
[ -n "$IDS" ] && $D inspect --format '{{.Name}}|{{.RestartCount}}|{{.State.OOMKilled}}|{{.State.ExitCode}}|{{.State.Running}}|{{.State.StartedAt}}|{{.State.FinishedAt}}' $IDS 2>/dev/null
fi
`, j, incidentMaxEvents)
}

// Incident reads what the server wrote down in a time range of up to three days (RFC 3339 times).
func (s *ServerService) Incident(sid, from, until string) (ServerIncident, error) {
	fj, _, f, err := logTime(from)
	if err != nil {
		return ServerIncident{}, err
	}
	uj, _, u, err := logTime(until)
	if err != nil {
		return ServerIncident{}, err
	}
	if !u.After(f) {
		return ServerIncident{}, errors.New("the end of the time range is before its start")
	}
	if u.Sub(f) > incidentMaxSpan {
		return ServerIncident{}, errors.New("the time range can be three days at most")
	}
	out, err := s.runTimeout(sid, incidentScript(fj, uj), serverLongTimeout)
	if err != nil && out == "" {
		return ServerIncident{}, err
	}
	return parseIncident(out, f, u), nil
}

var (
	isoLine    = regexp.MustCompile(`^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d[+-]\d{4}) (.*)$`)
	bucketLine = regexp.MustCompile(`^\s*(\d+) (\d{4}-\d\d-\d\dT\d\d:\d\d[+-]\d{4})$`)
	oomText    = regexp.MustCompile(`(?i)out of memory|oom-kill|oom_reaper|killed process`)
)

func incidentKind(text string) string {
	low := strings.ToLower(text)
	switch {
	case oomText.MatchString(text):
		return "oom"
	case strings.Contains(low, "kernel: linux version"):
		return "boot"
	case strings.Contains(low, "systemd-logind"):
		return "shutdown"
	case strings.Contains(low, "scheduled restart job"):
		return "restart"
	}
	return "failed"
}

func sections(out string) map[string][]string {
	m := map[string][]string{}
	cur := ""
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.HasPrefix(l, "##") {
			cur = l[2:]
			continue
		}
		if cur != "" && strings.TrimSpace(l) != "" {
			m[cur] = append(m[cur], l)
		}
	}
	return m
}

func parseIncident(out string, from, until time.Time) ServerIncident {
	sec := sections(out)
	inc := ServerIncident{Events: []IncidentEvent{}, Errors: []IncidentBucket{}, Containers: []IncidentBox{}}
	inc.Limited = len(sec["access"]) > 0 && sec["access"][0] == "limited"
	inc.Persistent = len(sec["persistent"]) > 0 && sec["persistent"][0] == "yes"
	if l := sec["oldest"]; len(l) > 0 {
		if m := isoLine.FindStringSubmatch(l[0]); m != nil {
			if at, err := time.Parse("2006-01-02T15:04:05-0700", m[1]); err == nil {
				inc.Oldest = at.UTC().Format(time.RFC3339)
			}
		}
	}
	for _, l := range sec["events"] {
		m := isoLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		at, err := time.Parse("2006-01-02T15:04:05-0700", m[1])
		if err != nil {
			continue
		}
		inc.Events = append(inc.Events, IncidentEvent{Time: at.UTC().Format(time.RFC3339), Kind: incidentKind(m[2]), Text: cut(m[2], 400)})
	}
	inc.Truncated = len(sec["events"]) >= incidentMaxEvents
	sort.SliceStable(inc.Events, func(i, j int) bool { return inc.Events[i].Time < inc.Events[j].Time })
	for _, l := range sec["errors"] {
		m := bucketLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		at, err := time.Parse("2006-01-02T15:04-0700", m[2])
		n, _ := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			continue
		}
		inc.Errors = append(inc.Errors, IncidentBucket{Minute: at.UTC().Format(time.RFC3339), Count: n})
	}
	sort.SliceStable(inc.Errors, func(i, j int) bool { return inc.Errors[i].Minute < inc.Errors[j].Minute })
	for i, l := range sec["docker"] {
		if i == 0 && l == "present" {
			inc.HasDocker = true
			continue
		}
		if b, ok := parseIncidentBox(l, from, until); ok {
			inc.Containers = append(inc.Containers, b)
		}
	}
	return inc
}

// parseIncidentBox reads a line of `docker inspect` and keeps the container only if something happened to it in the range.
func parseIncidentBox(l string, from, until time.Time) (IncidentBox, bool) {
	p := strings.Split(l, "|")
	if len(p) != 7 {
		return IncidentBox{}, false
	}
	restarts, _ := strconv.Atoi(p[1])
	code, _ := strconv.Atoi(p[3])
	b := IncidentBox{Name: strings.TrimPrefix(p[0], "/"), Restarts: restarts, OOM: p[2] == "true", ExitCode: code, Running: p[4] == "true"}
	var started, finished time.Time
	if at, err := time.Parse(time.RFC3339Nano, p[5]); err == nil && at.Year() > 1 {
		started, b.Started = at, at.UTC().Format(time.RFC3339)
	}
	if at, err := time.Parse(time.RFC3339Nano, p[6]); err == nil && at.Year() > 1 {
		finished = at
		if !b.Running {
			b.Finished = at.UTC().Format(time.RFC3339)
		}
	}
	in := func(at time.Time) bool { return !at.IsZero() && !at.Before(from) && !at.After(until) }
	if in(started) || in(finished) { // a restart moves the start, so a container that restarted in the range is here

		return b, true
	}
	return IncidentBox{}, false
}

// JournalKeep makes the journal persistent: it is written to disk, so it survives a reboot. Without that directory systemd
// keeps it in memory and a restart takes away exactly the lines that explain it.
func (s *ServerService) JournalKeep(sid string) error {
	_, err := s.run(sid, asRoot+`$SUDO mkdir -p /var/log/journal && $SUDO systemd-tmpfiles --create --prefix /var/log/journal 2>/dev/null; $SUDO systemctl restart systemd-journald`)
	s.audit(sid, "made the journal persistent", err)
	return err
}
