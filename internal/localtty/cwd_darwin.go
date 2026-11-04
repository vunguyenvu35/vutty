//go:build darwin

package localtty

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// processCwd is the working directory of a process, or "" when it cannot be read. macOS has no /proc: lsof
// (part of the system) prints it as a line "n<path>".
func processCwd(pid int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") {
			return line[1:]
		}
	}
	return ""
}
