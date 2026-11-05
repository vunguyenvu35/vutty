package usecase

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// serveRun answers a command run in a pod without a terminal, as a pod with sh, stat and cat would: a folder /app with a few files.
func (f *fakeCluster) serveRun(w http.ResponseWriter, r *http.Request) {
	up := websocket.Upgrader{Subprotocols: []string{"v4.channel.k8s.io"}}
	c, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()
	cmd := r.URL.Query()["command"] // sh -c script sh args...
	send := func(ch byte, s string) { _ = c.WriteMessage(websocket.BinaryMessage, append([]byte{ch}, s...)) }
	exit := func(code string) {
		if code == "0" {
			send(3, `{"status":"Success"}`)
			return
		}
		send(3, `{"status":"Failure","reason":"NonZeroExitCode","details":{"causes":[{"reason":"ExitCode","message":"`+code+`"}]}}`)
	}
	if r.URL.Query().Get("container") == "distroless" {
		send(3, `{"status":"Failure","message":"OCI runtime exec failed: exec failed: unable to start container process: exec: \"sh\": executable file not found in $PATH: unknown"}`)
		return
	}
	if len(cmd) < 4 {
		exit("2")
		return
	}
	script, arg := cmd[2], ""
	if len(cmd) > 4 {
		arg = cmd[4]
	}
	switch {
	case script == "pwd":
		send(1, "/app\n")
		exit("0")
	case strings.HasPrefix(script, "cd --") && arg == "/app":
		send(1, "directory|4096|1762300000|drwxr-xr-x|storage\n"+
			"regular file|120|1762300100|-rw-r--r--|.env\n"+
			"regular file|2048|1762300200|-rw-r--r--|report|2026.log\n"+ // a | in a name
			"directory|40|1762300300|drwxr-xr-x|..data\n"+
			"regular file|7|1762300400|-rw-r--r--|Notes.txt\n"+
			"not a line\n"+
			"regular file|abc|1|-rw-r--r--|bad-size\n")
		exit("0")
	case strings.HasPrefix(script, "cd --"):
		send(2, "cannot open "+arg+"\n")
		exit("3")
	case strings.HasPrefix(script, "stat -L -c '%F|%s'"):
		switch arg {
		case "/app/Notes.txt":
			send(1, "regular file|7\n")
		case "/app/storage":
			send(1, "directory|4096\n")
		case "/app/flaky.bin":
			send(1, "regular file|11\n")
		case "/app/huge.bin":
			send(1, "regular file|999999999999\n")
		case "/dev/null":
			send(1, "character special file|0\n")
		default:
			send(2, "stat: can't stat '"+arg+"': No such file or directory\n")
			exit("1")
			return
		}
		exit("0")
	case strings.HasPrefix(script, "exec cat"):
		if arg == "/app/Notes.txt" {
			send(1, "hello\n")
			send(1, "\n")
			exit("0")
			return
		}
		send(1, "half a file")
		send(2, "cat: read error: Input/output error\n")
		exit("1")
	default:
		exit("2")
	}
}

func podShell(t *testing.T, h *harness, container string) (hostID string, sid string) {
	t.Helper()
	f := newFakeCluster(t)
	sid, _ = clusterSession(t, h, f, "{token: tok}")
	sh, err := h.app.Server.K8sShellSession(sid, "prod", "shop", "api-7d9f6c-x2kq8", container)
	if err != nil {
		t.Fatal(err)
	}
	return sh.HostID, sid
}

func TestPodFilesListFolderAndHome(t *testing.T) {
	h := newHarness(t).unlocked(t)
	host, _ := podShell(t, h, "api")
	s := h.app.Server
	if home, err := s.PodFilesHome(host); err != nil || home != "/app" {
		t.Fatalf("home = %q, %v", home, err)
	}
	list, err := s.PodFilesList(host, "/app")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name)
	}
	// folders first, then by name; the | in a name is whole; a line that is not a file and a size that is no number are left out
	if got := strings.Join(names, ","); got != "..data,storage,.env,Notes.txt,report|2026.log" {
		t.Fatalf("names = %s", got)
	}
	if e := list[1]; !e.IsDir || e.Path != "/app/storage" || e.Mode != "drwxr-xr-x" {
		t.Fatalf("storage = %+v", e)
	}
	if e := list[4]; e.IsDir || e.Size != 2048 || e.ModTime.Unix() != 1762300200 || e.Path != "/app/report|2026.log" {
		t.Fatalf("report = %+v", e)
	}
	if _, err := s.PodFilesList(host, "/nope"); err == nil || !strings.Contains(err.Error(), "cannot open /nope") {
		t.Fatalf("a folder that cannot be opened says so: %v", err)
	}
}

func TestPodFilesFetchCopiesOneFileAndNeverOverwrites(t *testing.T) {
	h := newHarness(t).unlocked(t)
	host, _ := podShell(t, h, "api")
	s := h.app.Server
	dir := t.TempDir()
	got, err := s.PodFilesFetch(host, "/app/Notes.txt", dir)
	if err != nil || got != filepath.Join(dir, "Notes.txt") {
		t.Fatalf("%q, %v", got, err)
	}
	if b, _ := os.ReadFile(got); string(b) != "hello\n\n" {
		t.Fatalf("content = %q", b)
	}
	if st, _ := os.Stat(got); st.Mode().Perm() != 0o600 {
		t.Fatalf("a copy is readable by this user only: %v", st.Mode())
	}
	again, err := s.PodFilesFetch(host, "/app/Notes.txt", dir)
	if err != nil || again != filepath.Join(dir, "Notes (1).txt") {
		t.Fatalf("a second copy has its own name: %q, %v", again, err)
	}
	if b, _ := os.ReadFile(got); string(b) != "hello\n\n" {
		t.Fatal("the first copy was overwritten")
	}
	for path, want := range map[string]string{
		"/app/storage":  "folder cannot be copied",
		"/dev/null":     "not a regular file",
		"/app/huge.bin": "copies files up to 256 MB",
		"/app/missing":  "No such file",
	} {
		if _, err := s.PodFilesFetch(host, path, dir); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v (want %q)", path, err, want)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("a refused file leaves nothing behind: %d files", len(entries))
	}
}

func TestPodFilesFetchRemovesAHalfFile(t *testing.T) {
	h := newHarness(t).unlocked(t)
	host, _ := podShell(t, h, "api")
	dir := t.TempDir()
	// the pod sends the start of the file and then fails to read it
	if _, err := h.app.Server.PodFilesFetch(host, "/app/flaky.bin", dir); err == nil || !strings.Contains(err.Error(), "Input/output error") {
		t.Fatalf("the pod's own words: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a half file was left behind: %v", entries)
	}
}

func TestPodFilesRefuseWhatIsNotAPodShellOrAPath(t *testing.T) {
	h := newHarness(t).unlocked(t)
	host, _ := podShell(t, h, "api")
	s := h.app.Server
	dir := t.TempDir()
	for _, bad := range []string{"", "app", "../etc", "/a\nb", "/a\x00b", "/" + strings.Repeat("a", 5000)} {
		if _, err := s.PodFilesList(host, bad); err == nil {
			t.Errorf("list %q was not refused", bad)
		}
		if _, err := s.PodFilesFetch(host, bad, dir); err == nil {
			t.Errorf("fetch %q was not refused", bad)
		}
	}
	for _, id := range []string{"", "local", "kubeexec:", "kubeexec:nope", "some-host-id"} {
		if _, err := s.PodFilesHome(id); err == nil {
			t.Errorf("%q was taken for a pod shell", id)
		}
		if _, err := s.PodFilesList(id, "/"); err == nil {
			t.Errorf("list of %q", id)
		}
	}
	for _, d := range []string{"", "relative", filepath.Join(dir, "not-there")} {
		if _, err := s.PodFilesFetch(host, "/app/Notes.txt", d); err == nil {
			t.Errorf("a folder %q of this computer was taken", d)
		}
	}
}

func TestPodFilesSayWhenTheContainerHasNoShell(t *testing.T) {
	h := newHarness(t).unlocked(t)
	host, _ := podShell(t, h, "distroless")
	if _, err := h.app.Server.PodFilesList(host, "/app"); err == nil || !strings.Contains(err.Error(), "has no shell") {
		t.Fatalf("%v", err)
	}
	if _, err := h.app.Server.PodFilesHome(host); err == nil || !strings.Contains(err.Error(), "has no shell") {
		t.Fatalf("%v", err)
	}
}

func TestStatListParsing(t *testing.T) {
	got := parseStatList("/", "directory|1|2|drwx------|etc\nregular file|3|4|-rw-------|a b\n\n|||||\n")
	if len(got) != 2 || got[0].Path != "/etc" || got[1].Path != "/a b" {
		t.Fatalf("%+v", got)
	}
}

// The pod chooses the name of a file: it must land in the chosen folder whatever it is called.
func TestLocalFileNameCannotLeaveTheFolderOrNameADevice(t *testing.T) {
	for in, want := range map[string]string{
		"report.txt":       "report.txt",
		`..\..\evil.exe`:   ".._.._evil.exe",
		`C:\Windows\x.dll`: "C__Windows_x.dll",
		"a:b":              "a_b",
		"NUL":              "_NUL",
		"con.txt":          "_con.txt",
		"name. ":           "name",
		"...":              "file",
		"tab\tx\x00":       "tab_x_",
	} {
		if got := localFileName(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	for _, in := range []string{`..\..\x`, `\\srv\share\x`, "x/../../y", strings.Repeat("a", 500)} {
		got := localFileName(in)
		if strings.ContainsAny(got, `\/:`) || len(got) > 200 {
			t.Errorf("%q → %q", in, got)
		}
	}
}
