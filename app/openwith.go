package app

import (
	"os"
	"path/filepath"
	"strings"
)

// OpenApp is an application that can open a file: Path goes to OpenPathWith, Name is what the menu shows.
type OpenApp struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// maxOpenApps keeps the menu short; "Other…" reaches the rest.
const maxOpenApps = 12

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// OpenWithApps lists the installed applications that can open a file called name (the first is the one the system uses).
// Only the name's extension matters, so a remote file need not be downloaded to ask. The list is empty where the system
// cannot say, and for files that run programs, which are never opened from here: the UI then asks for an application.
func (s *SystemService) OpenWithApps(name string) ([]OpenApp, error) {
	ext := strings.ToLower(filepath.Ext(filepath.Base(name)))
	if riskyExt[ext] {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "vutty-type-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	probe := filepath.Join(dir, "file"+ext)
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return nil, err
	}
	var out []OpenApp
	for _, p := range installedAppsFor(probe) {
		out = append(out, OpenApp{Name: strings.TrimSuffix(filepath.Base(p), ".app"), Path: p})
		if len(out) == maxOpenApps {
			break
		}
	}
	return out, nil
}
