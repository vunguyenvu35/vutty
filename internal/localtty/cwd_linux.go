//go:build linux

package localtty

import (
	"os"
	"strconv"
)

// processCwd is the working directory of a process, or "" when it cannot be read.
func processCwd(pid int) string {
	dir, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
	if err != nil {
		return ""
	}
	return dir
}
