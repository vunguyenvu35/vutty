package usecase

import (
	"os"
	"path/filepath"
	"testing"

	"vutty/internal/domain"
)

func TestResetSettingsKeepsAccountAndSyncTarget(t *testing.T) {
	h := newHarness(t).unlocked(t)
	s := h.app.Settings.Get()
	s.Zoom, s.FontSize, s.Theme = 140, 20, "light"
	s.AccountName, s.AccountEmail = "Vu", "vu@example.com"
	s.Shortcuts = map[string]string{"new_connection": "Mod+T"}
	if _, err := h.app.Settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Vault.SetAutoLock(5); err != nil {
		t.Fatal(err)
	}
	h.core.settings.SyncURL = "git@example.com:me/sync.git"

	got, err := h.app.Settings.ResetSettings()
	if err != nil {
		t.Fatal(err)
	}
	def := defaultSettings()
	if got.Zoom != def.Zoom || got.FontSize != def.FontSize || got.Theme != def.Theme || got.AutoLockMinutes != def.AutoLockMinutes || len(got.Shortcuts) != 0 {
		t.Fatalf("preferences must be the defaults: %+v", got)
	}
	if got.AccountName != "Vu" || got.AccountEmail != "vu@example.com" || got.SyncURL != "git@example.com:me/sync.git" {
		t.Fatalf("the account and the sync target are kept: %+v", got)
	}
	if b, err := os.ReadFile(filepath.Join(h.core.DataDir, "settings.json")); err != nil || len(b) == 0 {
		t.Fatalf("the reset is saved: %v", err)
	}
	if again := h.app.Settings.Get(); again.Zoom != def.Zoom {
		t.Fatalf("Get after reset: %+v", again)
	}
}

func TestClearCacheRemovesCacheFilesAndForgetsTheAITools(t *testing.T) {
	h := newHarness(t).unlocked(t)
	cache := filepath.Join(t.TempDir(), "vutty")
	if err := os.MkdirAll(filepath.Join(cache, "helper"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "helper", "a"), make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "b"), make([]byte, 20), 0o600); err != nil {
		t.Fatal(err)
	}
	h.core.CacheDir = cache
	data := filepath.Join(h.core.DataDir, "settings.json")
	_ = os.WriteFile(data, []byte("{}"), 0o600)
	h.app.Assistant.detected = true

	r, err := h.app.Settings.ClearCache()
	if err != nil {
		t.Fatal(err)
	}
	if r.Files != 2 || r.Bytes != 120 {
		t.Fatalf("report: %+v", r)
	}
	if left, _ := os.ReadDir(cache); len(left) != 0 {
		t.Fatalf("the cache folder must be empty: %v", left)
	}
	if _, err := os.Stat(data); err != nil {
		t.Fatalf("user data must stay: %v", err)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("the folder itself stays: %v", err)
	}
}

func TestClearCacheWithoutAFolderIsFine(t *testing.T) {
	h := newHarness(t).unlocked(t)
	if r, err := h.app.Settings.ClearCache(); err != nil || r.Files != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	h.core.CacheDir = filepath.Join(t.TempDir(), "missing")
	if r, err := h.app.Settings.ClearCache(); err != nil || r.Files != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestConfirmProdIsOnByDefaultAndTheChoiceSurvivesARestartAndAReset(t *testing.T) {
	h := newHarness(t).unlocked(t)
	if !h.app.Settings.Get().ConfirmProd {
		t.Fatal("the production check must be on to begin with")
	}
	in := h.app.Settings.Get()
	in.ConfirmProd = false
	if _, err := h.app.Settings.Save(in); err != nil {
		t.Fatal(err)
	}
	c2, err := testCore(t, h.core.Dir, nil) // a restart: settings.json is read again
	if err != nil {
		t.Fatal(err)
	}
	if New(c2).Settings.Get().ConfirmProd {
		t.Fatal("an off switch came back on after a restart")
	}
	got, err := h.app.Settings.ResetSettings()
	if err != nil || !got.ConfirmProd {
		t.Fatalf("a reset goes back to the default (on): %+v %v", got, err)
	}
}

func TestSettingsWrittenByAnOlderVersionGetTheProductionCheckOn(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data", "settings.json"), []byte(`{"theme":"light","zoom":110}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := testCore(t, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := New(c).Settings.Get()
	if got.Theme != "light" || got.Zoom != 110 || !got.ConfirmProd {
		t.Fatalf("%+v", got)
	}
}

func TestLockOnSleepIsOnByDefaultAndTheChoiceSurvivesARestartAndAReset(t *testing.T) {
	h := newHarness(t).unlocked(t)
	if !h.app.Settings.Get().LockOnSleep {
		t.Fatal("locking when the computer sleeps must be on to begin with")
	}
	in := h.app.Settings.Get()
	in.LockOnSleep = false
	if _, err := h.app.Settings.Save(in); err != nil {
		t.Fatal(err)
	}
	c2, err := testCore(t, h.core.Dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if New(c2).Settings.Get().LockOnSleep {
		t.Fatal("an off switch came back on after a restart")
	}
	if got, err := h.app.Settings.ResetSettings(); err != nil || !got.LockOnSleep {
		t.Fatalf("a reset goes back to the default (on): %+v %v", got, err)
	}
}

func TestSettingsOfAnOlderVersionLockOnSleep(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "data"), 0o700)
	os.WriteFile(filepath.Join(dir, "data", "settings.json"), []byte(`{"theme":"light"}`), 0o600)
	c, err := testCore(t, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !New(c).Settings.Get().LockOnSleep {
		t.Fatal("a file from before this setting must get the default: on")
	}
}

func TestLockBecauseLocksAnOpenVaultOnceAndSaysWhy(t *testing.T) {
	h := newHarness(t).unlocked(t)
	h.app.Vault.LockBecause("the computer went to sleep")
	if h.app.Vault.Status().Unlocked {
		t.Fatal("still open")
	}
	h.app.Vault.LockBecause("again") // already locked: no second line
	n := 0
	logs, _ := h.app.Log.List(domain.ActivityFilter{})
	for _, l := range logs {
		if l.Message == "Vault locked: the computer went to sleep" {
			n++
		}
		if l.Message == "Vault locked: again" {
			t.Fatal("a locked vault was locked again in the Logs")
		}
	}
	if n != 1 {
		t.Fatalf("%d lines", n)
	}
}
