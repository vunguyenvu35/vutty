//go:build !darwin

package app

import "errors"

// Replacing the running program is done for macOS only so far: elsewhere the update is downloaded from the release page.
func platformSelfUpdate() (bool, string) {
	return false, "updating by itself is available on macOS only: download the new version from the release page"
}

func platformExtract(zipPath, dir string) (string, error) {
	return "", errors.New("not supported on this system")
}

func platformApply(newApp string) error { return errors.New("not supported on this system") }

// CleanUpAfterUpdate has nothing to remove here.
func CleanUpAfterUpdate(dir string) {}
