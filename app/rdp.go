package app

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"

	"vutty/internal/usecase"
)

// rdpCommand is the command that starts this system's Remote Desktop client on a host: mstsc on Windows, the app that opens
// .rdp files on a Mac (Microsoft Remote Desktop), FreeRDP or Remmina on Linux. lookPath finds a program (exec.LookPath in the app).
func rdpCommand(goos string, s usecase.RDPSpec, lookPath func(string) (string, error)) (*exec.Cmd, error) {
	switch goos {
	case "windows":
		return exec.Command("mstsc", s.File), nil
	case "darwin":
		return exec.Command("open", s.File), nil
	}
	addr := s.Host + ":" + strconv.Itoa(s.Port)
	for _, bin := range []string{"xfreerdp3", "xfreerdp", "wlfreerdp3", "wlfreerdp"} {
		if p, err := lookPath(bin); err == nil {
			args := []string{"/v:" + addr, "/cert:tofu", "/dynamic-resolution", "+clipboard"}
			if s.User != "" {
				args = append(args, "/u:"+s.User)
			}
			return exec.Command(p, args...), nil
		}
	}
	if p, err := lookPath("remmina"); err == nil {
		return exec.Command(p, "-c", s.File), nil
	}
	return nil, errors.New("no Remote Desktop client found: install FreeRDP (xfreerdp) or Remmina")
}

// launchRDP starts the client and returns without waiting for it to end. On a Mac, `open` fails when no app handles .rdp files.
func launchRDP(s usecase.RDPSpec) error {
	cmd, err := rdpCommand(runtime.GOOS, s, exec.LookPath)
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("no app opened the .rdp file: install Microsoft Remote Desktop (free in the App Store): %s", trimOut(out))
		}
		return nil
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // not left as a zombie
	return nil
}

func trimOut(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
