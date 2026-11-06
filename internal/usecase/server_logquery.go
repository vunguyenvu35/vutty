package usecase

import (
	"errors"
	"fmt"
	"time"
)

// serverLogSince maps the time ranges the screen offers to what each tool takes: journalctl, and docker (also compose).
var serverLogSince = map[string][2]string{
	"15m": {"15 min ago", "15m"},
	"1h":  {"1 hour ago", "1h"},
	"24h": {"24 hours ago", "24h"},
}

// logQuery is what a log is asked for, already checked: the pieces of it that reach a command are a number and times written
// by this program, never text from the screen.
type logQuery struct {
	lines  int
	jSince string // journalctl: " --since ... --until ..." (empty for no time)
	dSince string // docker and compose: the same for their flags
	boot   bool   // the boot before the present one (the journal only)
	ranged bool   // a time or a boot was asked for: a log with no time to ask for refuses it
	follow bool   // keep reading as the log grows
}

// logTime reads an RFC 3339 time and writes it the way journalctl (in UTC) and docker take it.
func logTime(v string) (journal, docker string, at time.Time, err error) {
	at, err = time.Parse(time.RFC3339, v)
	if err != nil {
		return "", "", at, errors.New("that is not a time the log can show")
	}
	u := at.UTC()
	return u.Format("2006-01-02 15:04:05") + " UTC", u.Format("2006-01-02T15:04:05Z"), at, nil
}

func logQueryOf(lines int, since, from, until string, boot bool) (logQuery, error) {
	switch lines {
	case 100, 300, 1000, 5000:
	default:
		return logQuery{}, errors.New("that is not a number of lines the log can show")
	}
	q := logQuery{lines: lines, boot: boot, ranged: boot}
	if since != "" {
		w, ok := serverLogSince[since]
		if !ok {
			return logQuery{}, errors.New("that is not a time the log can show")
		}
		q.jSince, q.dSince = fmt.Sprintf(" --since %q", w[0]), " --since "+w[1]
	}
	var f, u time.Time
	if from != "" {
		j, d, at, err := logTime(from)
		if err != nil {
			return logQuery{}, err
		}
		f, q.ranged = at, true
		q.jSince += fmt.Sprintf(" --since %q", j)
		q.dSince += " --since " + d
	}
	if until != "" {
		j, d, at, err := logTime(until)
		if err != nil {
			return logQuery{}, err
		}
		u, q.ranged = at, true
		q.jSince += fmt.Sprintf(" --until %q", j)
		q.dSince += " --until " + d
	}
	if !f.IsZero() && !u.IsZero() && u.Before(f) {
		return logQuery{}, errors.New("the end of the time range is before its start")
	}
	if boot && (from != "" || until != "") {
		return logQuery{}, errors.New("choose a time range or the previous boot, not both")
	}
	return q, nil
}

// logCommand is the fixed command that reads a log. kind is "" (the whole system), service, stack, web or container.
func logCommand(kind, name string, q logQuery) (string, error) {
	jb := ""
	if q.boot {
		jb = " -b -1"
	}
	follow := ""
	if q.follow {
		follow = " -f"
	}
	var cmd string
	switch {
	case name == "" && kind == "":
		cmd = fmt.Sprintf("journalctl%s -n %d%s%s --no-pager -o short-iso 2>&1", follow, q.lines, jb, q.jSince)
		return cmd, nil
	case !nameRe.MatchString(name):
		return "", errors.New("that is not a valid name")
	case kind == "service":
		cmd = fmt.Sprintf("journalctl -u %s.service%s -n %d%s%s --no-pager -o short-iso 2>&1", name, follow, q.lines, jb, q.jSince)
	case kind == "stack":
		if !composeNameRe.MatchString(name) {
			return "", errors.New("that is not a Compose project name")
		}
		if q.boot {
			return "", errors.New("Docker keeps no log by boot")
		}
		cmd = composeScript(name, fmt.Sprintf("logs%s --tail %d%s --timestamps --no-color", follow, q.lines, q.dSince))
	case kind == "web":
		if cmd = webLogCommand(name, q); cmd == "" {
			return "", errors.New("that is not a web server")
		}
		if name == "nginx" && q.ranged {
			return "", errors.New("the nginx error file has no times to choose from")
		}
	case kind == "container":
		if q.boot {
			return "", errors.New("Docker keeps no log by boot")
		}
		cmd = fmt.Sprintf("(docker logs%[4]s --tail %[1]d%[3]s --timestamps %[2]s 2>&1 || $SUDO docker logs%[4]s --tail %[1]d%[3]s --timestamps %[2]s 2>&1)", q.lines, name, q.dSince, follow)
	default:
		return "", errors.New("unknown kind of log")
	}
	return cmd, nil
}
