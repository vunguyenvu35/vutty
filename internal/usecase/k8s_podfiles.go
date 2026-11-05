package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"vutty/internal/domain"
)

// The files inside a pod, read only, for the Files section of the terminal sidebar when the terminal is a shell in a pod of a cluster
// opened with a kubeconfig ("kubeexec:<id>"). Nothing is installed in the pod: the folder is listed and a file is read by running
// `sh`, `stat` and `cat` there, through the same exec call as the shell, so a container without them (distroless, scratch) has no files
// to show. Every path goes in as an argument of the command, never inside its text.

const (
	podFilesListTimeout = 30 * time.Second
	podFilesMaxDir      = 20000 << 10 // the listing of one folder, in bytes of output
	podFilesMaxFile     = 256 << 20   // the largest file that is read
	podFilesFetchTime   = 10 * time.Minute
)

// podRunner is what a command in the pod of a shell needs.
type podRunner struct {
	api                     KubeAPI
	ns, pod, container, ctx string
}

func (s *ServerService) podOf(hostID string) (podRunner, error) {
	if !strings.HasPrefix(hostID, kubeExecPrefix) {
		return podRunner{}, errors.New("this terminal is not a shell in a pod")
	}
	t, ok := s.c.kubeShell(hostID)
	if !ok {
		return podRunner{}, errors.New("this shell is no longer known: open it again from the pod")
	}
	api, err := t.d.api(t.ctx)
	if err != nil {
		return podRunner{}, err
	}
	container := t.container
	if container == "" { // the one kubectl would take, as for the shell
		if container, err = t.d.defaultContainer(t.ctx, t.ns, t.pod); err != nil {
			return podRunner{}, err
		}
	}
	return podRunner{api: api, ns: t.ns, pod: t.pod, container: container, ctx: t.ctx}, nil
}

// run runs `sh -c script sh args...` in the pod and says why when it ended badly: the pod's own words, or that there is no shell.
func (p podRunner) run(limit time.Duration, w io.Writer, script string, args ...string) error {
	c, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	res, err := p.api.Run(c, p.ns, p.pod, p.container, append([]string{"sh", "-c", script, "sh"}, args...), w)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = "the command ended with exit code " + strconv.Itoa(res.ExitCode)
		}
		return errors.New(cut(msg, 300))
	}
	return nil
}

// limitWriter stops a command that writes more than it may.
type limitWriter struct {
	w    io.Writer
	left int64
}

var errTooMuch = errors.New("that is larger than Vutty reads from a pod")

func (l *limitWriter) Write(b []byte) (int, error) {
	if int64(len(b)) > l.left {
		return 0, errTooMuch
	}
	l.left -= int64(len(b))
	return l.w.Write(b)
}

// podPath is a path of the pod as the screen may send it: absolute, without a NUL or a line break.
func podPath(p string) error {
	if !strings.HasPrefix(p, "/") || len(p) > 4096 || strings.ContainsAny(p, "\x00\n\r") {
		return errors.New("that is not a path in the pod")
	}
	return nil
}

// PodFilesHome is the folder a command starts in in the pod (the working directory of the container).
func (s *ServerService) PodFilesHome(hostID string) (string, error) {
	p, err := s.podOf(hostID)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := p.run(podFilesListTimeout, &limitWriter{&out, 4096}, `pwd`); err != nil {
		return "", podShellError(err)
	}
	dir := strings.TrimSpace(out.String())
	if podPath(dir) != nil {
		return "/", nil
	}
	return dir, nil
}

// podShellError says plainly that the container has no shell when that is what the pod answered.
func podShellError(err error) error {
	m := err.Error()
	if strings.Contains(m, "executable file not found") || strings.Contains(m, "no such file or directory") && strings.Contains(m, "sh") {
		return errors.New("this container has no shell, so its files cannot be listed (distroless and scratch images have none)")
	}
	return err
}

// k8sStatFormat is what `stat -L` prints for each file: type, size, modification time (epoch), permissions as ls shows them, name. The
// name is last, so a name with a | in it is whole.
const k8sStatFormat = `%F|%s|%Y|%A|%n`

// PodFilesList lists a folder of the pod: folders first, then by name, as SFTP lists one. A link is shown as what it points to.
func (s *ServerService) PodFilesList(hostID, dir string) ([]domain.RemoteEntry, error) {
	if err := podPath(dir); err != nil {
		return nil, err
	}
	p, err := s.podOf(hostID)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	script := `cd -- "$1" 2>/dev/null || { echo "cannot open $1" >&2; exit 3; }
stat -L -c '` + k8sStatFormat + `' -- * .[!.]* ..?* 2>/dev/null
exit 0`
	if err := p.run(podFilesListTimeout, &limitWriter{&out, podFilesMaxDir}, script, dir); err != nil {
		return nil, podShellError(err)
	}
	return parseStatList(dir, out.String()), nil
}

func parseStatList(dir, out string) []domain.RemoteEntry {
	var list []domain.RemoteEntry
	for _, ln := range strings.Split(out, "\n") {
		f := strings.SplitN(strings.TrimRight(ln, "\r"), "|", 5)
		if len(f) != 5 || f[4] == "" || f[4] == "." || f[4] == ".." {
			continue
		}
		size, err1 := strconv.ParseInt(f[1], 10, 64)
		mod, err2 := strconv.ParseInt(f[2], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		list = append(list, domain.RemoteEntry{Name: f[4], Path: path.Join(dir, f[4]), Size: size, Mode: f[3], ModTime: time.Unix(mod, 0), IsDir: f[0] == "directory"})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].IsDir != list[j].IsDir {
			return list[i].IsDir
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	return list
}

// PodFilesFetch copies one file of the pod into a folder of this computer (never over a file that is there: a number is added to the
// name) and returns where it went. A folder, a file of another kind and a file over 256 MB are refused.
func (s *ServerService) PodFilesFetch(hostID, remote, localDir string) (string, error) {
	if err := podPath(remote); err != nil {
		return "", err
	}
	if !filepath.IsAbs(localDir) {
		return "", errors.New("that is not a folder of this computer")
	}
	if st, err := os.Stat(localDir); err != nil || !st.IsDir() {
		return "", errors.New("that is not a folder of this computer")
	}
	name := path.Base(remote)
	if name == "/" || name == "." || name == ".." || name == "" {
		return "", errors.New("that is not a file")
	}
	name = localFileName(name)
	p, err := s.podOf(hostID)
	if err != nil {
		return "", err
	}
	var info bytes.Buffer
	if err := p.run(podFilesListTimeout, &limitWriter{&info, 1024}, `stat -L -c '%F|%s' -- "$1"`, remote); err != nil {
		return "", podShellError(err)
	}
	kind, sizeText, _ := strings.Cut(strings.TrimSpace(info.String()), "|")
	size, _ := strconv.ParseInt(sizeText, 10, 64)
	switch {
	case kind == "directory":
		return "", errors.New("a folder cannot be copied out of a pod yet: open it and copy its files")
	case !strings.Contains(kind, "regular"):
		return "", errors.New("that is not a regular file (" + kind + ")")
	case size > podFilesMaxFile:
		return "", fmt.Errorf("that file is %d MB: Vutty copies files up to %d MB out of a pod", size>>20, podFilesMaxFile>>20)
	}
	f, dest, err := createUnique(localDir, name)
	if err != nil {
		return "", err
	}
	werr := p.run(podFilesFetchTime, &limitWriter{f, podFilesMaxFile}, `exec cat -- "$1"`, remote)
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(dest) // a half file is not left behind
		return "", werr
	}
	return dest, nil
}

// createUnique creates a file that does not exist yet in dir, readable by this user only: name, then "name (1)", "name (2)", ...
func createUnique(dir, name string) (*os.File, string, error) {
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		n := name
		if i > 0 {
			n = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		dest := filepath.Join(dir, n)
		f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return f, dest, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("there are too many files with that name in the folder")
}

// localFileName makes the name a file of a pod gets on this computer. The pod chooses the name, and a name that is one word to the pod
// can be a path here (a \ is a separator on Windows, "C:x" names a drive, "NUL" a device): what the systems do not take in a name is
// replaced, so the file lands in the folder that was chosen and nowhere else.
func localFileName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`\/:*?"<>|`, r) {
			r = '_'
		}
		b.WriteRune(r)
	}
	out := strings.TrimRight(b.String(), ". ") // Windows drops a trailing dot or space: "x." and "x" would be one file
	if out == "" || strings.Trim(out, ".") == "" {
		out = "file"
	}
	stem, _, _ := strings.Cut(out, ".")
	switch strings.ToUpper(stem) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		out = "_" + out
	}
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}
