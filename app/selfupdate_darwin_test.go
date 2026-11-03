//go:build darwin

package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBundleRootOf(t *testing.T) {
	for in, want := range map[string]string{
		"/Applications/vutty.app/Contents/MacOS/vutty":     "/Applications/vutty.app",
		"/Users/me/Apps/My Vutty.app/Contents/MacOS/vutty": "/Users/me/Apps/My Vutty.app",
		"/Volumes/vutty 1.2/vutty.app/Contents/MacOS/x":    "/Volumes/vutty 1.2/vutty.app",
		"/a/b.app/Contents/MacOS/b.app/Contents/MacOS/c":   "/a/b.app", // the first one: the outer bundle
		"/usr/local/bin/vutty":                             "",
		"/Applications/vutty.app/Contents/Resources/x":     "",
		"": "",
	} {
		got, ok := bundleRootOf(in)
		if got != want || ok != (want != "") {
			t.Errorf("%q: got %q %v, want %q", in, got, ok, want)
		}
	}
}

// writeApp makes a small application bundle: its identity, a program and a marker that says which copy it is.
func writeApp(t *testing.T, dir, name, id, marker string, executable bool) string {
	t.Helper()
	app := filepath.Join(dir, name+".app")
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>%s</string></dict></plist>`, id)
	os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644)
	mode := os.FileMode(0o644)
	if executable {
		mode = 0o755
	}
	os.WriteFile(filepath.Join(app, "Contents", "MacOS", name), []byte("#!/bin/sh\n"), mode)
	os.WriteFile(filepath.Join(app, "marker"), []byte(marker), 0o644)
	return app
}

func read(p string) string { b, _ := os.ReadFile(p); return string(b) }

func TestCheckBundleOnlyLetsThroughThisApplicationInOneWholePiece(t *testing.T) {
	old := verifySignature
	defer func() { verifySignature = old }()
	verifySignature = func(string) error { return nil }
	dir := t.TempDir()
	cur := writeApp(t, dir, "vutty", "com.wails.vutty", "old", true)
	if err := checkBundle(writeApp(t, t.TempDir(), "vutty", "com.wails.vutty", "new", true), cur); err != nil {
		t.Fatalf("the right application was refused: %v", err)
	}
	if err := checkBundle(writeApp(t, t.TempDir(), "vutty", "com.example.other", "new", true), cur); err == nil || !strings.Contains(err.Error(), "not this application") {
		t.Fatalf("another application passed: %v", err)
	}
	if err := checkBundle(writeApp(t, t.TempDir(), "vutty", "com.wails.vutty", "new", false), cur); err == nil || !strings.Contains(err.Error(), "no program") {
		t.Fatalf("a program that cannot run passed: %v", err)
	}
	empty := filepath.Join(t.TempDir(), "vutty.app")
	os.MkdirAll(empty, 0o755)
	if err := checkBundle(empty, cur); err == nil {
		t.Fatal("an empty bundle passed")
	}
	verifySignature = func(string) error { return fmt.Errorf("the new version is damaged") }
	if err := checkBundle(writeApp(t, t.TempDir(), "vutty", "com.wails.vutty", "new", true), cur); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("a bundle whose signature does not check passed: %v", err)
	}
}

// startSwapAndWait runs the real helper script against a process that ends after a second, and waits for the open command it makes.
func startSwapAndWait(t *testing.T, staged, root, backup string) (opened string, early string) {
	t.Helper()
	dir := t.TempDir()
	rec := filepath.Join(dir, "opened")
	opener := filepath.Join(dir, "open.sh")
	os.WriteFile(opener, []byte("#!/bin/sh\necho \"$1\" > "+rec+"\n"), 0o755)
	sleeper := exec.Command("/bin/sleep", "1")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	go sleeper.Wait()
	if err := startSwap(sleeper.Process.Pid, staged, root, backup, []string{"OPEN_CMD=" + opener}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	early = read(root + "/marker") // the application is still running: the script must not have touched anything
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if b := read(rec); b != "" {
			return strings.TrimSpace(b), early
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the new application was never opened")
	return "", ""
}

func TestTheSwapScriptWaitsForTheAppToEndThenPutsTheNewCopyInPlaceAndKeepsTheOldOne(t *testing.T) {
	dir := t.TempDir()
	root := writeApp(t, dir, "vutty", "id", "old", true)
	staged := writeApp(t, dir, ".vutty.update", "id", "new", true)
	backup := root + ".previous"
	opened, early := startSwapAndWait(t, staged, root, backup)
	if early != "old" {
		t.Fatalf("the script swapped while the application was still running (marker %q)", early)
	}
	if opened != root {
		t.Fatalf("opened %q, want %q", opened, root)
	}
	if read(root+"/marker") != "new" || read(backup+"/marker") != "old" {
		t.Fatalf("after the swap: the app is %q and the kept copy %q", read(root+"/marker"), read(backup+"/marker"))
	}
	if _, err := os.Stat(staged); err == nil {
		t.Fatal("the staged copy is still there")
	}
}

func TestTheSwapScriptPutsTheOldCopyBackWhenTheNewOneCannotBeMovedIn(t *testing.T) {
	dir := t.TempDir()
	root := writeApp(t, dir, "vutty", "id", "old", true)
	gone := filepath.Join(dir, "does-not-exist.app") // the staged copy vanished
	backup := root + ".previous"
	opened, _ := startSwapAndWait(t, gone, root, backup)
	if opened != root || read(root+"/marker") != "old" {
		t.Fatalf("the old application was not put back: opened %q, marker %q", opened, read(root+"/marker"))
	}
}

func makeZip(t *testing.T, src string) string {
	t.Helper()
	zip := filepath.Join(t.TempDir(), "x.zip")
	if out, err := exec.Command("/usr/bin/ditto", "-c", "-k", "--keepParent", src, zip).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	return zip
}

func TestExtractKeepsTheProgramRunnableAndSymlinksAsTheyWere(t *testing.T) {
	app := writeApp(t, t.TempDir(), "vutty", "id", "zipped", true)
	if err := os.Symlink("MacOS/vutty", filepath.Join(app, "Contents", "link")); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	got, err := platformExtract(makeZip(t, app), out)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(out, "vutty.app") || read(got+"/marker") != "zipped" {
		t.Fatalf("%s", got)
	}
	if st, err := os.Stat(filepath.Join(got, "Contents", "MacOS", "vutty")); err != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("the program lost its permission to run: %v %v", st, err)
	}
	if target, err := os.Readlink(filepath.Join(got, "Contents", "link")); err != nil || target != "MacOS/vutty" {
		t.Fatalf("the link: %q %v", target, err)
	}
	// nothing that came from outside carries the quarantine flag that makes Gatekeeper ask
	if o, _ := exec.Command("/usr/bin/xattr", "-p", "com.apple.quarantine", got).CombinedOutput(); strings.Contains(string(o), "0081") || strings.Contains(string(o), "0083") {
		t.Fatalf("the unpacked application is quarantined: %s", o)
	}
}

func TestExtractRefusesAZipWithNoApplicationOrWithTwo(t *testing.T) {
	notApp := filepath.Join(t.TempDir(), "folder")
	os.MkdirAll(notApp, 0o755)
	os.WriteFile(filepath.Join(notApp, "readme"), []byte("x"), 0o644)
	if _, err := platformExtract(makeZip(t, notApp), t.TempDir()); err == nil {
		t.Fatal("a zip with no application was accepted")
	}
	two := t.TempDir()
	writeApp(t, two, "a", "id", "1", true)
	writeApp(t, two, "b", "id", "2", true)
	if _, err := platformExtract(makeZip(t, two), t.TempDir()); err == nil {
		t.Fatal("a zip with two applications was accepted")
	}
	bad := filepath.Join(t.TempDir(), "bad.zip")
	os.WriteFile(bad, []byte("not a zip"), 0o644)
	if _, err := platformExtract(bad, t.TempDir()); err == nil {
		t.Fatal("something that is not a zip was accepted")
	}
}

func TestAWholeUpdateWithTheRealUnpacker(t *testing.T) {
	built := writeApp(t, t.TempDir(), "vutty", "id", "v1.3.0", true)
	zipBytes, _ := os.ReadFile(makeZip(t, built))
	f := newFakeRelease(t, "v1.3.0")
	f.zip = zipBytes
	r := f.installer(t, "1.2.1")
	r.in.extract = platformExtract
	if err := r.in.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.applied) != 1 || read(r.applied[0]+"/marker") != "v1.3.0" {
		t.Fatalf("%v", r.applied)
	}
}

func TestOnlyAnInstalledApplicationInAFolderYouCanWriteToIsOfferedTheUpdate(t *testing.T) {
	// the test binary is not inside an application bundle, so it must say so
	ok, reason := platformSelfUpdate()
	if ok || reason == "" {
		t.Fatalf("a program outside a bundle was offered an update: %v %q", ok, reason)
	}
	if !writable(t.TempDir()) || writable("/System") || writable(filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("writable() is wrong")
	}
}
