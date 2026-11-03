package usecase

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vutty/internal/domain"
)

// logOf runs chunks through a line writer with a fixed clock and returns what would be in the file.
func logOf(chunks ...string) string {
	var sb strings.Builder
	t := time.Date(2026, 11, 4, 10, 30, 0, 0, time.Local)
	w := &lineWriter{out: &sb, now: func() time.Time { return t }}
	for _, c := range chunks {
		w.Write([]byte(c))
	}
	w.Flush()
	return sb.String()
}

func TestLineWriterWritesLinesWithTheirTime(t *testing.T) {
	if got := logOf("first\r\nsecond\n"); got != "10:30:00 first\n10:30:00 second\n" {
		t.Fatalf("%q", got)
	}
	if got := logOf("a   \n\n  b\n"); got != "10:30:00 a\n\n10:30:00   b\n" { // trailing spaces go, a blank line stays blank
		t.Fatalf("%q", got)
	}
	if got := logOf("$ prompt without an end"); got != "10:30:00 $ prompt without an end\n" { // what a session stopped at
		t.Fatalf("%q", got)
	}
}

func TestLineWriterDropsColoursAndTitlesAndKeepsTheText(t *testing.T) {
	cases := map[string]string{
		"\x1b[31mred\x1b[0m text\n":      "10:30:00 red text\n",
		"\x1b[1;32mbold green\x1b[m\n":   "10:30:00 bold green\n",
		"\x1b]0;user@host: ~\x07$ ls\n":  "10:30:00 $ ls\n",  // window title, ended by BEL
		"\x1b]2;title\x1b\\$ pwd\n":      "10:30:00 $ pwd\n", // ended by ESC \
		"\x1b(Bplain\x1b)0 text\n":       "10:30:00 plain text\n",
		"\x1b[?2004hprompt\x1b[?2004l\n": "10:30:00 prompt\n", // bracketed paste on and off
		"bell\x07 here\n":                "10:30:00 bell here\n",
		"tab\there\n":                    "10:30:00 tab\there\n",
		"caf\u00e9 \u4f60\u597d\n":       "10:30:00 caf\u00e9 \u4f60\u597d\n",
	}
	for in, want := range cases {
		if got := logOf(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestLineWriterDoesWhatTheTerminalDidToTheLine(t *testing.T) {
	cases := map[string]string{
		"progress 10%\rprogress 50%\rprogress 100%\n": "10:30:00 progress 100%\n", // a carriage return overwrites
		"ab\r\ncd\n":                       "10:30:00 ab\n10:30:00 cd\n",
		"ls -x\b \bl\n":                    "10:30:00 ls -l\n",       // a backspace echo: erase the x, type an l
		"prompt$ old\rprompt$ new\x1b[K\n": "10:30:00 prompt$ new\n", // a shell redrawing its line
		"keep this\rX\x1b[K\n":             "10:30:00 X\n",
		"abcdef\x1b[3D\x1b[K\n":            "10:30:00 abc\n", // back three, erase to the end
		"abcdef\x1b[2K\rnew\n":             "10:30:00 new\n",
		"abcdef\r\x1b[2Pxy\n":              "10:30:00 xyef\n", // delete two characters, then type over the start
	}
	for in, want := range cases {
		if got := logOf(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestLineWriterIsNotConfusedByAChunkBoundary(t *testing.T) {
	if got := logOf("\x1b[3", "1mred\x1b[0", "m\n"); got != "10:30:00 red\n" { // an escape sequence cut in two places
		t.Fatalf("%q", got)
	}
	e := []byte("caf\u00e9\n")
	if got := logOf(string(e[:4]), string(e[4:])); got != "10:30:00 caf\u00e9\n" { // a character cut in the middle
		t.Fatalf("%q", got)
	}
	if got := logOf("\x1b]0;ti", "tle\x07$ x\n"); got != "10:30:00 $ x\n" {
		t.Fatalf("%q", got)
	}
}

func TestLineWriterMarksANewDayAndCutsALineThatNeverEnds(t *testing.T) {
	var sb strings.Builder
	now := time.Date(2026, 11, 4, 23, 59, 59, 0, time.Local)
	w := &lineWriter{out: &sb, now: func() time.Time { return now }}
	w.Write([]byte("late\n"))
	now = now.Add(2 * time.Second)
	w.Write([]byte("early\n"))
	if got := sb.String(); got != "23:59:59 late\n# 2026-11-05\n00:00:01 early\n" {
		t.Fatalf("%q", got)
	}
	sb.Reset()
	w.Write([]byte(strings.Repeat("x", sessionLogMaxLine+10) + "\n"))
	if !strings.HasPrefix(sb.String(), "00:00:01 xxx") || strings.Count(sb.String(), "\n") < 2 {
		t.Fatalf("a line without an end was not cut: %d bytes", len(sb.String()))
	}
}

// ---- the file ----

func (h *harness) logging(t *testing.T, mode, dir string) {
	t.Helper()
	in := h.app.Settings.Get()
	in.SessionLog, in.SessionLogDir = mode, dir
	if _, err := h.app.Settings.Save(in); err != nil {
		t.Fatal(err)
	}
}

func readLogs(t *testing.T, dir string) (names []string, contents string) {
	t.Helper()
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		names = append(names, e.Name())
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		contents += string(b)
	}
	return
}

func TestASessionIsWrittenToATextFileWithTheHostAndTheEnd(t *testing.T) {
	h := newHarness(t).unlocked(t)
	dir := filepath.Join(t.TempDir(), "logs")
	h.logging(t, SessionLogAll, dir)
	srv := startServer(t)
	host := h.sshHost(t, "web 1", srv, "")
	sid, _ := h.open(host.ID, 80, 24)
	h.trust(t, sid)
	h.rec.waitOutput(t, sid, "READY")
	if err := h.app.Session.Write(sid, "hello world\r"); err != nil {
		t.Fatal(err)
	}
	h.rec.waitOutput(t, sid, "hello world")
	time.Sleep(50 * time.Millisecond)
	if err := h.app.Session.Close(sid); err != nil {
		t.Fatal(err)
	}
	names, body := readLogs(t, dir)
	if len(names) != 1 || !strings.HasSuffix(names[0], ".log") || !strings.Contains(names[0], "web_1") {
		t.Fatalf("files %v", names)
	}
	for _, want := range []string{"# Vutty session log", "# host: web 1 (root@127.0.0.1:", "# started: ", "READY", "hello world", "# ended: ", "(closed by you)"} {
		if !strings.Contains(body, want) {
			t.Errorf("the log lacks %q:\n%s", want, body)
		}
	}
	st, _ := os.Stat(filepath.Join(dir, names[0]))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("the log is %v: it holds what a server printed", st.Mode().Perm())
	}
	if d, _ := os.Stat(dir); d.Mode().Perm() != 0o700 {
		t.Fatalf("the folder is %v", d.Mode().Perm())
	}
	// the Logs page says where it went
	logs, _ := h.app.Log.List(domain.ActivityFilter{})
	found := false
	for _, l := range logs {
		if strings.Contains(l.Message, "Logging this session to "+dir) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no mention in the Logs: %+v", logs)
	}
}

func TestNoLogIsWrittenWhenItIsOffOrForAHostThatIsNotProductionInProdMode(t *testing.T) {
	h := newHarness(t).unlocked(t)
	dir := filepath.Join(t.TempDir(), "logs")
	srv := startServer(t)
	host := h.sshHost(t, "web", srv, "")
	first := true
	run := func() {
		sid, _ := h.open(host.ID, 80, 24)
		if first { // the host key is asked about once, then it is trusted
			h.trust(t, sid)
			first = false
		}
		h.rec.waitOutput(t, sid, "READY")
		time.Sleep(50 * time.Millisecond)
		h.app.Session.Close(sid)
	}
	h.logging(t, SessionLogOff, dir)
	run()
	if names, _ := readLogs(t, dir); len(names) != 0 {
		t.Fatalf("a log although logging is off: %v", names)
	}
	h.logging(t, SessionLogProd, dir)
	host.Env = domain.EnvDev
	h.app.Host.SaveHost(host)
	run()
	if names, _ := readLogs(t, dir); len(names) != 0 {
		t.Fatalf("a log of a host that is not production: %v", names)
	}
	host.Env = domain.EnvProd
	h.app.Host.SaveHost(host)
	run()
	if names, _ := readLogs(t, dir); len(names) != 1 {
		t.Fatalf("no log of a production host: %v", names)
	}
}

func TestAFolderThatCannotBeUsedDoesNotStopTheTerminalAndTheLogsSayWhy(t *testing.T) {
	h := newHarness(t).unlocked(t)
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, []byte("x"), 0o600)
	h.logging(t, SessionLogAll, filepath.Join(blocker, "logs")) // a folder under a file
	srv := startServer(t)
	host := h.sshHost(t, "web", srv, "")
	sid, _ := h.open(host.ID, 80, 24)
	h.trust(t, sid)
	h.rec.waitOutput(t, sid, "READY") // the terminal works
	logs, _ := h.app.Log.List(domain.ActivityFilter{})
	for _, l := range logs {
		if !l.OK && strings.Contains(l.Message, "Could not write the session log") {
			return
		}
	}
	t.Fatalf("the Logs do not say why: %+v", logs)
}

func TestASessionThatNeverPrintsLeavesNoFile(t *testing.T) {
	h := newHarness(t).unlocked(t)
	dir := filepath.Join(t.TempDir(), "logs")
	h.logging(t, SessionLogAll, dir)
	host := h.sshHost(t, "nowhere", startServer(t), "")
	sid, _ := h.open(host.ID, 80, 24)
	h.app.Session.Close(sid) // closed while still connecting
	time.Sleep(50 * time.Millisecond)
	if names, _ := readLogs(t, dir); len(names) != 0 {
		t.Fatalf("an empty log: %v", names)
	}
}

func TestSessionLogSettingsAreCheckedAndKept(t *testing.T) {
	h := newHarness(t)
	s := h.app.Settings
	if got := s.Get(); got.SessionLog != SessionLogOff || got.SessionLogDir != "" {
		t.Fatalf("defaults %+v", got)
	}
	if f := s.SessionLogFolder(); f != filepath.Join(h.core.DataDir, "session-logs") {
		t.Fatalf("default folder %q", f)
	}
	bad := map[string]func(*Settings){
		"mode":     func(x *Settings) { x.SessionLog = "sometimes" },
		"relative": func(x *Settings) { x.SessionLogDir = "logs/here" },
		"nul":      func(x *Settings) { x.SessionLogDir = "/tmp/a\x00b" },
		"long":     func(x *Settings) { x.SessionLogDir = "/" + strings.Repeat("a", 1100) },
	}
	for name, mutate := range bad {
		in := goodSettings()
		mutate(&in)
		if _, err := s.Save(in); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	in := goodSettings()
	in.SessionLog, in.SessionLogDir = SessionLogAll, "/var/log/vutty"
	if got, err := s.Save(in); err != nil || got.SessionLog != SessionLogAll || s.SessionLogFolder() != "/var/log/vutty" {
		t.Fatalf("%+v %v", got, err)
	}
	in.SessionLog = "" // an older client that does not send it: off
	if got, _ := s.Save(in); got.SessionLog != SessionLogOff {
		t.Fatalf("%+v", got)
	}
}
