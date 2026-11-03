package usecase

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"vutty/internal/domain"
)

// Session log modes (Settings.SessionLog).
const (
	SessionLogOff  = "off"
	SessionLogProd = "prod" // only the terminals of hosts marked prod
	SessionLogAll  = "all"
)

const (
	sessionLogMaxBytes = 50 << 20 // a session's file stops growing here
	sessionLogMaxLine  = 64 << 10 // a line that never ends is cut here
)

// sessionLogDir is where the logs go: the folder in Settings, else <data>/session-logs.
func (c *Core) sessionLogDir() string {
	c.setMu.Lock()
	dir := c.settings.SessionLogDir
	c.setMu.Unlock()
	if dir != "" {
		return dir
	}
	return filepath.Join(c.DataDir, "session-logs")
}

// sessionLogFor returns the log of a session, or nil when the settings do not ask for one. Nothing is created yet: the file is
// opened with the first output, so a connection that never gets that far leaves nothing behind.
func (c *Core) sessionLogFor(sess *session) *sessionLog {
	c.setMu.Lock()
	mode := c.settings.SessionLog
	c.setMu.Unlock()
	if mode != SessionLogAll && mode != SessionLogProd {
		return nil
	}
	label, target, prod := "This computer", "", false
	switch id := sess.hostID; {
	case id == LocalHostID:
	case strings.HasPrefix(id, "serial:"):
		label = strings.TrimPrefix(id, "serial:")
	default:
		hostID := strings.TrimPrefix(id, telnetPrefix)
		h, err := c.Store.GetHost(hostID)
		if err != nil {
			return nil
		}
		label, target, prod = h.Label, fmt.Sprintf("%s@%s:%d", h.Username, h.Hostname, h.Port), h.Env == domain.EnvProd
		if strings.HasPrefix(id, telnetPrefix) {
			target = "telnet " + h.Hostname
		}
	}
	if mode == SessionLogProd && !prod {
		return nil
	}
	return &sessionLog{c: c, hostID: sess.hostID, label: label, target: target, dir: c.sessionLogDir(), now: time.Now}
}

// sessionLog is the file a terminal session is written to: plain text, one line at a time with the time it was completed, what the
// host printed and nothing else (a typed password is never echoed, and keystrokes are not recorded as such).
type sessionLog struct {
	c      *Core
	hostID string
	label  string
	target string
	dir    string
	now    func() time.Time

	mu      sync.Mutex
	f       *os.File
	lw      *lineWriter
	written int64
	capped  bool
	failed  bool
	closed  bool
	path    string
}

func fileSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if out := strings.Trim(b.String(), "._"); out != "" {
		if len(out) > 40 {
			out = out[:40]
		}
		return out
	}
	return "session"
}

// open creates the file with the first output. The caller holds l.mu.
func (l *sessionLog) open() {
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		l.fail(err)
		return
	}
	started := l.now()
	name := fmt.Sprintf("%s_%s_%s.log", started.Format("20060102-150405"), fileSafe(l.label), domain.NewID()[:6])
	l.path = filepath.Join(l.dir, name)
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		l.fail(err)
		return
	}
	l.f = f
	l.lw = &lineWriter{out: lineSink{l}, now: l.now}
	head := "# Vutty session log\n# host: " + l.label
	if l.target != "" {
		head += " (" + l.target + ")"
	}
	head += "\n# started: " + started.Format(time.RFC3339) + "\n# This is what the host printed, line by line. What you type is not recorded, except what the host echoes back.\n\n"
	l.rawWrite([]byte(head))
	l.c.record(domain.ActConnect, l.hostID, l.label, "Logging this session to "+l.path, true)
}

func (l *sessionLog) fail(err error) {
	l.failed = true
	l.c.record(domain.ActConnect, l.hostID, l.label, "Could not write the session log: "+err.Error(), false)
}

// Write is where the output of the session arrives; it never blocks the terminal on a problem with the file.
func (l *sessionLog) Write(p []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.failed || l.capped {
		return
	}
	if l.f == nil {
		l.open()
		if l.f == nil {
			return
		}
	}
	l.lw.Write(p)
}

// rawWrite writes to the file; the caller holds l.mu.
func (l *sessionLog) rawWrite(p []byte) (int, error) {
	if l.f == nil || l.capped {
		return len(p), nil
	}
	if l.written+int64(len(p)) > sessionLogMaxBytes {
		l.capped = true
		_, _ = l.f.WriteString("\n# The log stopped here: it reached the size limit (50 MB).\n")
		return len(p), nil
	}
	n, err := l.f.Write(p)
	l.written += int64(n)
	if err != nil {
		l.capped = true
		l.c.record(domain.ActConnect, l.hostID, l.label, "The session log stopped: "+err.Error(), false)
	}
	return len(p), nil
}

// Close writes what was left on the last line and the end of the log. It may be called again.
func (l *sessionLog) Close(reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.closed = true
	if l.f == nil {
		return
	}
	l.lw.Flush()
	if !l.capped {
		l.rawWrite([]byte("\n# ended: " + l.now().Format(time.RFC3339) + " (" + reason + ")\n"))
	}
	_ = l.f.Close()
}

// lineWriter turns what a terminal is sent into the lines a person would have seen: colours and other escape sequences are dropped,
// a carriage return moves back to the start of the line (what follows overwrites), a backspace steps back, and "erase to the end of
// the line" really erases, so a shell that redraws its prompt while you type leaves one clean line. Each finished line is written
// with the time it ended.
type lineWriter struct {
	out   io.Writer
	now   func() time.Time
	line  []rune
	col   int
	state lwState
	param []byte
	part  []byte // the start of a multi-byte character that has not arrived whole
	day   string
}

type lwState int

const (
	lwText lwState = iota
	lwEsc
	lwCSI
	lwString  // OSC, DCS, … until BEL or ESC \
	lwStringE // an ESC inside such a string
	lwSkip    // the one byte that follows ESC ( and its kind
)

func (w *lineWriter) Write(p []byte) {
	if len(w.part) > 0 {
		p = append(w.part, p...)
		w.part = nil
	}
	for i := 0; i < len(p); {
		b := p[i]
		switch w.state {
		case lwEsc:
			i++
			switch b {
			case '[':
				w.state, w.param = lwCSI, w.param[:0]
			case ']', 'P', 'X', '^', '_':
				w.state = lwString
			case '(', ')', '*', '+', '-', '.', '/', '#', '%':
				w.state = lwSkip
			default:
				w.state = lwText
			}
		case lwSkip:
			i++
			w.state = lwText
		case lwCSI:
			i++
			if b >= 0x40 && b <= 0x7e {
				w.csi(b)
				w.state = lwText
			} else if len(w.param) < 32 {
				w.param = append(w.param, b)
			}
		case lwString:
			i++
			switch b {
			case 0x07:
				w.state = lwText
			case 0x1b:
				w.state = lwStringE
			}
		case lwStringE:
			i++
			w.state = lwText // ESC \ ends the string; anything else ends it as well
		default:
			switch {
			case b == 0x1b:
				i++
				w.state = lwEsc
			case b == '\n':
				i++
				w.flushLine()
			case b == '\r':
				i++
				w.col = 0
			case b == '\b':
				i++
				if w.col > 0 {
					w.col--
				}
			case b == '\t':
				i++
				w.put('\t')
			case b < 0x20 || b == 0x7f:
				i++ // bell and the like
			default:
				if !utf8.FullRune(p[i:]) {
					w.part = append([]byte(nil), p[i:]...)
					return
				}
				r, n := utf8.DecodeRune(p[i:])
				i += n
				if r != utf8.RuneError || n > 1 {
					w.put(r)
				}
			}
		}
	}
}

func (w *lineWriter) put(r rune) {
	if w.col < len(w.line) {
		w.line[w.col] = r
	} else {
		for len(w.line) < w.col {
			w.line = append(w.line, ' ')
		}
		w.line = append(w.line, r)
	}
	w.col++
	if len(w.line) >= sessionLogMaxLine {
		w.flushLine()
	}
}

// csi handles the few control sequences that change what is on the line.
func (w *lineWriter) csi(final byte) {
	n := 1
	if v, ok := atoiBytes(w.param); ok && v > 0 {
		n = v
	}
	switch final {
	case 'K': // erase in line
		switch string(w.param) {
		case "", "0":
			if w.col < len(w.line) {
				w.line = w.line[:w.col]
			}
		case "1":
			for i := 0; i < w.col && i < len(w.line); i++ {
				w.line[i] = ' '
			}
		case "2":
			w.line = w.line[:0]
		}
	case 'P': // delete characters
		if w.col < len(w.line) {
			end := w.col + n
			if end > len(w.line) {
				end = len(w.line)
			}
			w.line = append(w.line[:w.col], w.line[end:]...)
		}
	case 'D': // cursor left
		if w.col -= n; w.col < 0 {
			w.col = 0
		}
	case 'C': // cursor right
		if w.col += n; w.col > sessionLogMaxLine {
			w.col = sessionLogMaxLine
		}
	case 'G': // cursor to column
		w.col = n - 1
	}
}

func atoiBytes(b []byte) (int, bool) {
	if len(b) == 0 || len(b) > 5 {
		return 0, false
	}
	v := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false // a sequence with other parameters is not the plain one this reads
		}
		v = v*10 + int(c-'0')
	}
	return v, true
}

func (w *lineWriter) flushLine() {
	text := strings.TrimRight(string(w.line), " ")
	w.line, w.col = w.line[:0], 0
	if text == "" {
		_, _ = w.out.Write([]byte("\n"))
		return
	}
	t := w.now()
	if day := t.Format("2006-01-02"); day != w.day {
		if w.day != "" {
			_, _ = w.out.Write([]byte("# " + day + "\n"))
		}
		w.day = day
	}
	_, _ = w.out.Write([]byte(t.Format("15:04:05") + " " + text + "\n"))
}

// Flush writes the line that has no end yet (the prompt a session stopped at).
func (w *lineWriter) Flush() {
	if len(w.line) > 0 {
		w.flushLine()
	}
}

// lineSink lets the line writer write to the log without going back through Write (which holds the lock already).
type lineSink struct{ l *sessionLog }

func (s lineSink) Write(p []byte) (int, error) { return s.l.rawWrite(p) }
