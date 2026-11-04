package usecase

import (
	"encoding/json"
	"testing"
	"time"

	"vutty/internal/domain"
)

func setPrefs(t *testing.T, h *harness, f func(*Settings)) {
	t.Helper()
	s := h.app.Settings.Get()
	f(&s)
	if _, err := h.app.Settings.Save(s); err != nil {
		t.Fatal(err)
	}
}

func group(t *testing.T, at time.Time, v any) domain.PrefGroup {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return domain.PrefGroup{UpdatedAt: at, Values: b}
}

func TestPreferencesFollowTheUserToANewMachineAndBetweenMachines(t *testing.T) {
	remote := bareRepo(t)
	a := newHarness(t).unlocked(t)
	setPrefs(t, a, func(s *Settings) {
		s.Theme, s.FontSize, s.Scrollback, s.Language = "light", 17, 4000, "vi"
		s.AccountName, s.AccountEmail = "Vu", "vu@example.com"
		s.Shortcuts = map[string]string{"new_connection": "Mod+T"}
		s.Zoom = 140 // belongs to this screen
	})
	if err := a.app.Sync.Configure(remote); err != nil {
		t.Fatal(err)
	}
	if err := a.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}

	b := newHarness(t)
	if err := b.app.Sync.Configure(remote); err != nil {
		t.Fatal(err)
	}
	if err := b.app.Sync.JoinExisting(masterPW); err != nil {
		t.Fatal(err)
	}
	got := b.app.Settings.Get()
	if got.Theme != "light" || got.FontSize != 17 || got.Scrollback != 4000 || got.Language != "vi" || got.AccountName != "Vu" || got.AccountEmail != "vu@example.com" || got.Shortcuts["new_connection"] != "Mod+T" {
		t.Fatalf("a new machine takes the preferences: %+v", got)
	}
	if got.Zoom == 140 {
		t.Fatal("zoom belongs to one machine and must not travel")
	}

	// a change on B reaches A
	time.Sleep(5 * time.Millisecond)
	setPrefs(t, b, func(s *Settings) { s.CursorStyle, s.Highlight = "bar", false })
	if err := b.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}
	if err := a.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}
	if g := a.app.Settings.Get(); g.CursorStyle != "bar" || g.Highlight || g.Theme != "light" || g.Zoom != 140 {
		t.Fatalf("A takes the newer preferences and keeps its own zoom: %+v", g)
	}
}

func TestOnlyTheTickedGroupsTravelInEitherDirection(t *testing.T) {
	remote := bareRepo(t)
	a := newHarness(t).unlocked(t)
	setPrefs(t, a, func(s *Settings) {
		s.Theme, s.FontSize, s.Language, s.AccountName = "light", 18, "vi", "Vu"
		s.SyncItems = []string{"appearance", "language"} // not the terminal, not the account
	})
	if err := a.app.Sync.Configure(remote); err != nil {
		t.Fatal(err)
	}
	if err := a.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}

	b := newHarness(t)
	if err := b.app.Sync.Configure(remote); err != nil {
		t.Fatal(err)
	}
	if err := b.app.Sync.JoinExisting(masterPW); err != nil {
		t.Fatal(err)
	}
	def := defaultSettings()
	if g := b.app.Settings.Get(); g.Theme != "light" || g.Language != "vi" || g.FontSize != def.FontSize || g.AccountName != "" {
		t.Fatalf("A sent only appearance and language: %+v", g)
	}

	// B unticks language: A's newer language is not taken, and B's terminal group (ticked) is
	time.Sleep(5 * time.Millisecond)
	setPrefs(t, b, func(s *Settings) { s.SyncItems = []string{"appearance", "terminal"}; s.FontSize = 22 })
	if err := b.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	setPrefs(t, a, func(s *Settings) { s.Language = "ja"; s.SyncItems = allPrefGroups() })
	if err := a.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}
	if err := b.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}
	if g := b.app.Settings.Get(); g.Language != "vi" {
		t.Fatalf("B did not tick language, so A's change must not arrive: %q", g.Language)
	}
	if g := a.app.Settings.Get(); g.FontSize != 22 {
		t.Fatalf("A ticked everything now, so it takes B's terminal group: %+v", g)
	}
}

func TestOnlyAChangeOfAGroupMovesItsDateAndTheNewestCopyWins(t *testing.T) {
	h := newHarness(t).unlocked(t)
	if len(h.app.Settings.Get().PrefsUpdated) != 0 {
		t.Fatal("never changed: no date")
	}
	setPrefs(t, h, func(s *Settings) { s.Zoom = 120 })
	if len(h.app.Settings.Get().PrefsUpdated) != 0 {
		t.Fatal("a setting that is not synced must not date a group")
	}
	setPrefs(t, h, func(s *Settings) { s.FontSize = 19 })
	d := h.app.Settings.Get().PrefsUpdated
	if d["terminal"].IsZero() || len(d) != 1 {
		t.Fatalf("only the terminal group changed: %v", d)
	}
	d1 := d["terminal"]
	term := func(size int) terminalPrefs {
		return terminalPrefs{FontSize: size, FontFamily: "Menlo", CursorStyle: "block", CursorBlink: true, Scrollback: 1000}
	}
	apply := func(at time.Time, size int) {
		h.core.applyPrefs(domain.Preferences{"terminal": group(t, at, term(size))})
	}

	apply(d1.Add(-time.Hour), 11)
	if h.app.Settings.Get().FontSize != 19 {
		t.Fatal("an older copy must lose")
	}
	apply(d1.Add(time.Hour), 999) // does not pass the checks of Save
	if h.app.Settings.Get().FontSize != 19 {
		t.Fatal("an invalid copy must be ignored")
	}
	apply(d1.Add(time.Hour), 11)
	if g := h.app.Settings.Get(); g.FontSize != 11 || !g.PrefsUpdated["terminal"].Equal(d1.Add(time.Hour)) {
		t.Fatalf("a newer copy must win: %+v", g)
	}
	h.core.applyPrefs(domain.Preferences{"nonsense": group(t, time.Now().Add(time.Hour), 1)}) // an unknown group is ignored
}

func TestNothingIsSentOrTakenForAnUntickedGroupAndDefaultsNeverOutvote(t *testing.T) {
	h := newHarness(t).unlocked(t)
	if h.core.outgoingPrefs() != nil {
		t.Fatal("a machine that kept every default sends nothing")
	}
	setPrefs(t, h, func(s *Settings) { s.FontSize = 18; s.SyncItems = []string{} })
	if h.core.outgoingPrefs() != nil {
		t.Fatal("nothing ticked: nothing is sent")
	}
	later := time.Now().Add(time.Hour).UTC()
	h.core.applyPrefs(domain.Preferences{"terminal": group(t, later, terminalPrefs{FontSize: 12, FontFamily: "Menlo", CursorStyle: "block", Scrollback: 1000})})
	if h.app.Settings.Get().FontSize != 18 {
		t.Fatal("nothing ticked: nothing is taken")
	}
	setPrefs(t, h, func(s *Settings) { s.SyncItems = []string{"terminal", "bogus", "terminal"} })
	if got := h.app.Settings.Get().SyncItems; len(got) != 1 || got[0] != "terminal" {
		t.Fatalf("unknown and repeated ids are dropped: %v", got)
	}
}

func TestAMachineSetUpBeforePreferencesSyncedDatesItsNonDefaultGroupsAtTheFirstSync(t *testing.T) {
	h := newHarness(t).unlocked(t)
	h.core.settings.FontSize = 21 // as read from an old settings.json: changed, but never dated
	p := h.core.outgoingPrefs()
	if len(p) != 1 || p["terminal"].UpdatedAt.IsZero() {
		t.Fatalf("only the group that differs from the default is sent: %+v", p)
	}
}

func TestMergeKeepsTheNewerCopyOfEachGroup(t *testing.T) {
	old := time.Unix(100, 0)
	neu := time.Unix(200, 0)
	a := domain.Preferences{"x": {UpdatedAt: old, Values: json.RawMessage(`1`)}, "y": {UpdatedAt: neu, Values: json.RawMessage(`2`)}}
	b := domain.Preferences{"x": {UpdatedAt: neu, Values: json.RawMessage(`3`)}, "z": {UpdatedAt: old, Values: json.RawMessage(`4`)}}
	m := domain.Merge(domain.Bundle{Prefs: a}, domain.Bundle{Prefs: b}).Prefs
	if string(m["x"].Values) != "3" || string(m["y"].Values) != "2" || string(m["z"].Values) != "4" {
		t.Fatalf("%v", m)
	}
	if m := domain.Merge(domain.Bundle{Prefs: b}, domain.Bundle{Prefs: a}).Prefs; string(m["x"].Values) != "3" {
		t.Fatal("newer wins, either way round")
	}
	if m := domain.Merge(domain.Bundle{}, domain.Bundle{Prefs: a}).Prefs; len(m) != 2 {
		t.Fatal("a side that sends none must not erase the other's")
	}
	if m := domain.PurgeTombstones(domain.Bundle{Prefs: a}, time.Now()); len(m.Prefs) != 2 {
		t.Fatal("purging tombstones keeps the preferences")
	}
}
