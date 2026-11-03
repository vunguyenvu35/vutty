//go:build darwin

package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// bundleRootOf returns the .app that an executable lives in ("/Applications/vutty.app" for ".../vutty.app/Contents/MacOS/vutty").
func bundleRootOf(exe string) (string, bool) {
	i := strings.Index(exe, ".app/Contents/MacOS/")
	if i < 0 {
		return "", false
	}
	return exe[:i+len(".app")], true
}

// runningBundle is the application this process belongs to.
func runningBundle() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return bundleRootOf(exe)
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".vutty-write-test-")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// platformSelfUpdate: this copy can replace itself when it is an application bundle in a folder the user can write to, on the kind
// of Mac that the release is built for.
func platformSelfUpdate() (bool, string) {
	if runtime.GOARCH != "arm64" {
		return false, "the update is built for Macs with Apple silicon: download this one from the release page"
	}
	root, ok := runningBundle()
	if !ok {
		return false, "this copy of Vutty is not an installed application (a development build)"
	}
	switch {
	case strings.Contains(root, "/AppTranslocation/"):
		return false, "macOS is running Vutty from a temporary place: move it to your Applications folder and open it from there"
	case strings.HasPrefix(root, "/Volumes/"):
		return false, "Vutty is running from a disk image: drag it to your Applications folder and open it from there"
	case !writable(filepath.Dir(root)):
		return false, "Vutty cannot write to " + filepath.Dir(root) + ": install it in your Applications folder, or somewhere you own"
	}
	return true, ""
}

// platformExtract unpacks the zip with ditto, which keeps symbolic links, permissions and signatures as they were packed.
func platformExtract(zipPath, dir string) (string, error) {
	if out, err := exec.Command("/usr/bin/ditto", "-x", "-k", zipPath, dir).CombinedOutput(); err != nil {
		return "", fmt.Errorf("could not unpack the update: %v: %s", err, strings.TrimSpace(string(out)))
	}
	apps, _ := filepath.Glob(filepath.Join(dir, "*.app"))
	if len(apps) != 1 {
		return "", errors.New("the update does not contain one application")
	}
	return apps[0], nil
}

func plistValue(app, key string) (string, error) {
	out, err := exec.Command("/usr/bin/plutil", "-extract", key, "raw", "-o", "-", filepath.Join(app, "Contents", "Info.plist")).Output()
	return strings.TrimSpace(string(out)), err
}

// verifySignature is codesign's own check that the unpacked bundle is whole (a damaged or half-unpacked one fails); a variable for tests.
var verifySignature = func(app string) error {
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", app).CombinedOutput(); err != nil {
		return fmt.Errorf("the new version is damaged: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// checkBundle makes sure what was unpacked is this application and nothing else, before it takes the place of the running one.
func checkBundle(newApp, currentRoot string) error {
	exe := filepath.Join(newApp, "Contents", "MacOS", strings.TrimSuffix(filepath.Base(currentRoot), ".app"))
	if st, err := os.Stat(exe); err != nil || st.Mode()&0o111 == 0 {
		return errors.New("the new version has no program in it")
	}
	want, err := plistValue(currentRoot, "CFBundleIdentifier")
	if err != nil || want == "" {
		return errors.New("could not read the identity of this application")
	}
	got, err := plistValue(newApp, "CFBundleIdentifier")
	if err != nil || got != want {
		return errors.New("the new version is not this application")
	}
	return verifySignature(newApp)
}

// swapScript waits for the running application to end, puts the new copy where the old one was and opens it. If it cannot, the old
// copy is put back. The old copy is kept next to it as <name>.previous until the new one has started (CleanUpAfterUpdate).
const swapScript = `#!/bin/sh
PID="$1"; NEW="$2"; OLD="$3"; BAK="$4"; OPEN="${OPEN_CMD:-/usr/bin/open}"
n=0
while kill -0 "$PID" 2>/dev/null; do
  n=$((n+1)); [ "$n" -gt 300 ] && exit 1
  sleep 0.2
done
rm -rf "$BAK"
mv "$OLD" "$BAK" || exit 1
if mv "$NEW" "$OLD"; then
  "$OPEN" "$OLD"
else
  mv "$BAK" "$OLD"; "$OPEN" "$OLD"; exit 1
fi
rm -f "$0"; rmdir "$(dirname "$0")" 2>/dev/null
exit 0
`

// startSwap writes the script and starts it on its own, so that it goes on after this process ends.
func startSwap(pid int, staged, root, backup string, env []string) error {
	dir, err := os.MkdirTemp("", "vutty-swap-")
	if err != nil {
		return err
	}
	script := filepath.Join(dir, "swap.sh")
	if err := os.WriteFile(script, []byte(swapScript), 0o700); err != nil {
		return err
	}
	cmd := exec.Command("/bin/sh", script, strconv.Itoa(pid), staged, root, backup)
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// platformApply puts the new application next to the running one (the same folder, so the swap is a rename) and starts the helper
// that replaces it once this process has quit.
func platformApply(newApp string) error {
	root, ok := runningBundle()
	if !ok {
		return errors.New("this copy of Vutty is not an installed application")
	}
	if err := checkBundle(newApp, root); err != nil {
		return err
	}
	staged := filepath.Join(filepath.Dir(root), "."+filepath.Base(root)+".update")
	os.RemoveAll(staged)
	if err := os.Rename(newApp, staged); err != nil { // another volume: copy it
		if out, err := exec.Command("/usr/bin/ditto", newApp, staged).CombinedOutput(); err != nil {
			return fmt.Errorf("could not put the new version next to this one: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}
	return startSwap(os.Getpid(), staged, root, root+".previous", nil)
}

// CleanUpAfterUpdate removes what an update leaves behind once the new version has been running for a while: the old copy kept as
// <name>.previous, a half-made <name>.update, and the update's working folder. It does nothing when there is nothing to remove.
func CleanUpAfterUpdate(workDir string) {
	root, ok := runningBundle()
	if !ok {
		return
	}
	go func() {
		time.Sleep(30 * time.Second) // a new version that dies at once leaves the old one to go back to
		for _, p := range []string{root + ".previous", filepath.Join(filepath.Dir(root), "."+filepath.Base(root)+".update")} {
			if _, err := os.Stat(filepath.Join(p, "Contents", "Info.plist")); err == nil {
				os.RemoveAll(p)
			}
		}
		if workDir != "" {
			os.RemoveAll(workDir)
		}
	}()
}
