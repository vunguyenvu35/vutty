package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"vutty/internal/localtty"
	"vutty/internal/usecase"
)

// moshDirs are where mosh-client is usually installed and a program started from the Dock or the Start menu does not look.
var moshDirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin", "/usr/bin", "/snap/bin"}

// findMoshClient finds mosh-client: on the PATH, else in the usual folders. lookPath and executable are exec.LookPath and a test of a
// file in the app.
func findMoshClient(lookPath func(string) (string, error), executable func(string) bool) (string, error) {
	if p, err := lookPath("mosh-client"); err == nil {
		return p, nil
	}
	for _, d := range moshDirs {
		if p := filepath.Join(d, "mosh-client"); executable(p) {
			return p, nil
		}
	}
	hint := "install mosh on this computer (brew install mosh on a Mac, your package manager on Linux)"
	if runtime.GOOS == "windows" {
		return "", errors.New("Mosh is not available on Windows")
	}
	return "", errors.New("mosh-client was not found: " + hint)
}

func isExecutable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// moshClientCommand is the command that connects mosh-client to a mosh-server. The key is in the environment, not in the arguments,
// where every user of this computer could read it.
func moshClientCommand(bin string, t usecase.MoshTarget) *exec.Cmd {
	cmd := exec.Command(bin, t.IP, strconv.Itoa(t.Port))
	cmd.Env = []string{"MOSH_KEY=" + t.Key}
	return cmd
}

func openMoshClient(t usecase.MoshTarget, cols, rows int, onData func([]byte)) (usecase.Shell, error) {
	bin, err := findMoshClient(exec.LookPath, isExecutable)
	if err != nil {
		return nil, err
	}
	sh, err := localtty.OpenCommand(moshClientCommand(bin, t), cols, rows, onData)
	if err != nil {
		return nil, err
	}
	return sh, nil
}
