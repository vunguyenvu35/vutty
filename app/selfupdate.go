package app

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"vutty/internal/updater"
)

// Self-update. The release has a list of checksums (SHA256SUMS) and a signature of that list (SHA256SUMS.sig) made with a key
// that only the maintainer holds; the app carries the public half. An update is installed only if the list is signed by that key
// and the downloaded file has the checksum the list gives for it. The file is fetched by the app itself, so macOS does not mark it
// as a download from a browser (Gatekeeper's quarantine flag), which is why only the very first install needs the "Open Anyway"
// step. What protects the update is the signature, not Apple: see docs/ReleaseSigning.md.

// UpdateProgress is what the screen is told while an update installs (the update:progress event).
type UpdateProgress struct {
	Stage string `json:"stage"` // checking, downloading, verifying, installing, restarting
	Done  int64  `json:"done,omitempty"`
	Total int64  `json:"total,omitempty"`
}

// SelfUpdateStatus says whether this copy of the app can replace itself, and if not, why.
type SelfUpdateStatus struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

const (
	sumsName = "SHA256SUMS"
	sigName  = "SHA256SUMS.sig"
)

var zipName = regexp.MustCompile(`^vutty-(\d+\.\d+\.\d+)-macos\.zip$`)

// installer is one update from the GitHub release to the moment the new app is handed over. What depends on the system (unpacking,
// checking the bundle, swapping it for the running one) comes in as functions, so the rest is the same everywhere and can be tested.
type installer struct {
	client  *updater.Client
	apiURL  string
	current string
	pub     ed25519.PublicKey
	workDir string
	// extract unpacks the zip into dir and returns the path of the application that came out of it.
	extract func(zipPath, dir string) (string, error)
	// apply replaces the running application with the new one and arranges the restart; the caller quits the app after it returns.
	apply    func(newApp string) error
	progress func(UpdateProgress)
}

func (in *installer) say(p UpdateProgress) {
	if in.progress != nil {
		in.progress(p)
	}
}

// run installs the latest release if it is newer than the running one. Nothing is changed on the way: the running app is only
// touched by apply, after every check has passed.
func (in *installer) run(ctx context.Context) error {
	in.say(UpdateProgress{Stage: "checking"})
	rel, err := in.client.LatestRelease(ctx, in.apiURL)
	if err != nil {
		return err
	}
	if rel.Draft || rel.Prerelease {
		return errors.New("the latest release is not a final one: update from its page")
	}
	version := strings.TrimPrefix(rel.Tag, "v")
	if !newerVersion(rel.Tag, in.current) {
		return errors.New("you are already on the latest version")
	}
	zipAsset, ok1 := rel.Asset("vutty-" + version + "-macos.zip")
	sumsAsset, ok2 := rel.Asset(sumsName)
	sigAsset, ok3 := rel.Asset(sigName)
	if !ok1 || !ok2 || !ok3 {
		return fmt.Errorf("version %s was published without what a self-update needs (the app, %s and %s): download it from the release page", version, sumsName, sigName)
	}
	if zipAsset.Size <= 0 || zipAsset.Size > updater.MaxAppFile {
		return errors.New("the update has an unexpected size")
	}

	// the list of checksums counts only if it is signed by the key this app was built with
	sums, err := in.client.Fetch(ctx, sumsAsset.URL, updater.MaxSmallFile)
	if err != nil {
		return err
	}
	sigText, err := in.client.Fetch(ctx, sigAsset.URL, updater.MaxSmallFile)
	if err != nil {
		return err
	}
	sig, err := updater.DecodeSignature(sigText)
	if err != nil || !updater.Verify(in.pub, sums, sig) {
		return errors.New("the signature of this release is not valid, so it was not installed")
	}
	list, err := updater.ParseChecksums(sums)
	if err != nil {
		return err
	}
	want, ok := list[zipAsset.Name]
	if !ok {
		return fmt.Errorf("the signed list does not have %s", zipAsset.Name)
	}
	if m := zipName.FindStringSubmatch(zipAsset.Name); m == nil || m[1] != version { // the version is in the signed name, not only in what GitHub says
		return errors.New("the update's name does not match its version")
	}

	if err := os.MkdirAll(in.workDir, 0o700); err != nil {
		return err
	}
	zipPath := filepath.Join(in.workDir, zipAsset.Name)
	in.say(UpdateProgress{Stage: "downloading", Total: zipAsset.Size})
	got, err := in.client.Download(ctx, zipAsset.URL, zipPath, updater.MaxAppFile, func(done, total int64) {
		if total <= 0 {
			total = zipAsset.Size
		}
		in.say(UpdateProgress{Stage: "downloading", Done: done, Total: total})
	})
	if err != nil {
		return err
	}
	in.say(UpdateProgress{Stage: "verifying"})
	if got != want {
		os.Remove(zipPath)
		return errors.New("the download does not match the signed checksum, so it was not installed")
	}

	in.say(UpdateProgress{Stage: "installing"})
	dir := filepath.Join(in.workDir, "unpacked")
	os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	newApp, err := in.extract(zipPath, dir)
	if err != nil {
		return err
	}
	if err := in.apply(newApp); err != nil {
		return err
	}
	in.say(UpdateProgress{Stage: "restarting"})
	return nil
}

// ---- the service side ----

var updateMu sync.Mutex

// SelfUpdateStatus says whether Install and restart is offered: only for a copy of the app that is in a place it can write to.
func (s *SystemService) SelfUpdateStatus() SelfUpdateStatus {
	if _, err := updater.PublicKey(); err != nil {
		return SelfUpdateStatus{Reason: "this build has no update key"}
	}
	ok, reason := platformSelfUpdate()
	return SelfUpdateStatus{Supported: ok, Reason: reason}
}

// InstallUpdate downloads the latest release, checks its signature and checksum, replaces this application with it and restarts.
// It returns only when the update failed (with the reason) or when the new copy is in place and the app is about to quit.
func (s *SystemService) InstallUpdate() error {
	if !updateMu.TryLock() {
		return errors.New("an update is already being installed")
	}
	defer updateMu.Unlock()
	if st := s.SelfUpdateStatus(); !st.Supported {
		return errors.New(firstNonEmpty(st.Reason, "this copy of the app cannot update itself"))
	}
	pub, err := updater.PublicKey()
	if err != nil {
		return err
	}
	url := latestReleaseURL
	if u := os.Getenv("VUTTY_UPDATE_URL"); u != "" {
		url = u
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	in := &installer{
		client: &updater.Client{UA: "vutty/" + Version}, apiURL: url, current: Version, pub: pub,
		workDir:  filepath.Join(s.updateDir, fmt.Sprintf("%d", time.Now().UnixNano())),
		extract:  platformExtract,
		apply:    platformApply,
		progress: func(p UpdateProgress) { s.emitUpdate(p) },
	}
	defer os.RemoveAll(in.workDir)
	if err := in.run(ctx); err != nil {
		return err
	}
	// the helper waits for this process to end, swaps the application and opens the new one
	go func() {
		time.Sleep(600 * time.Millisecond) // the screen shows "restarting" first
		if s.ctx != nil {
			runtime.Quit(s.ctx)
		}
	}()
	return nil
}

func (s *SystemService) emitUpdate(p UpdateProgress) {
	if s.ctx != nil {
		runtime.EventsEmit(s.ctx, "update:progress", p)
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
