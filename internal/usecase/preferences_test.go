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
		s.SyncWhat = []string{"appearance", "language"} // not the terminal, not the account
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
	setPrefs(t, b, func(s *Settings) { s.SyncWhat = []string{"appearance", "terminal"}; s.FontSize = 22 })
	if err := b.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	setPrefs(t, a, func(s *Settings) { s.Language = "ja"; s.SyncWhat = allSyncWhat() })
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
	setPrefs(t, h, func(s *Settings) { s.FontSize = 18; s.SyncWhat = []string{} })
	if h.core.outgoingPrefs() != nil {
		t.Fatal("nothing ticked: nothing is sent")
	}
	later := time.Now().Add(time.Hour).UTC()
	h.core.applyPrefs(domain.Preferences{"terminal": group(t, later, terminalPrefs{FontSize: 12, FontFamily: "Menlo", CursorStyle: "block", Scrollback: 1000})})
	if h.app.Settings.Get().FontSize != 18 {
		t.Fatal("nothing ticked: nothing is taken")
	}
	setPrefs(t, h, func(s *Settings) { s.SyncWhat = []string{"terminal", "bogus", "terminal"} })
	if got := h.app.Settings.Get().SyncWhat; len(got) != 1 || got[0] != "terminal" {
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

func TestAKindOfDataThatIsNotTickedIsNeitherSentNorTaken(t *testing.T) {
	remote := bareRepo(t)
	a := newHarness(t).unlocked(t)
	a.app.Snippet.Save(domain.Snippet{Label: "first", Script: "echo 1\n"})
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
	if len(b.app.Snippet.List()) != 1 {
		t.Fatal("a new machine takes everything, everything being ticked by default")
	}

	// B stops syncing snippets and hosts
	setPrefs(t, b, func(s *Settings) { s.SyncWhat = []string{syncKeys} })
	a.app.Snippet.Save(domain.Snippet{Label: "from A", Script: "echo A\n"})
	a.app.Host.SaveHost(domain.Host{Label: "host-from-a", Hostname: "a.example"})
	if err := a.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}
	own, _ := b.app.Snippet.Save(domain.Snippet{Label: "only on B", Script: "echo B\n"})
	b.app.Host.SaveHost(domain.Host{Label: "host-only-on-b", Hostname: "b.example"})
	if err := b.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}
	hasSnippet := func(h *harness, label string) bool {
		for _, x := range h.app.Snippet.List() {
			if x.Label == label {
				return true
			}
		}
		return false
	}
	hasHost := func(h *harness, label string) bool {
		for _, x := range h.app.Host.ListHosts(domain.HostFilter{}) {
			if x.Label == label {
				return true
			}
		}
		return false
	}
	if hasSnippet(b, "from A") || hasHost(b, "host-from-a") {
		t.Fatal("what is not ticked must not be taken")
	}
	if !hasSnippet(b, "only on B") || !hasHost(b, "host-only-on-b") || own.ID == "" {
		t.Fatal("what is not ticked stays here")
	}

	// the repository still holds A's data and none of B's unticked data: a third machine sees it
	c := newHarness(t)
	if err := c.app.Sync.Configure(remote); err != nil {
		t.Fatal(err)
	}
	if err := c.app.Sync.JoinExisting(masterPW); err != nil {
		t.Fatal(err)
	}
	if !hasSnippet(c, "from A") || !hasHost(c, "host-from-a") || hasSnippet(c, "only on B") || hasHost(c, "host-only-on-b") {
		t.Fatalf("the repository must keep A's and not get B's: %+v", c.app.Snippet.List())
	}
}

func TestSelectingWhatToSyncIsConsistent(t *testing.T) {
	if got := cleanSyncWhat([]string{"forwards", "language", "bogus", "language", "snippets"}); len(got) != 2 || got[0] != "snippets" || got[1] != "language" {
		t.Fatalf("forwards need hosts, unknown ids and repeats go, the order is fixed: %v", got)
	}
	if got := cleanSyncWhat([]string{"forwards", "hosts"}); len(got) != 2 || got[0] != "hosts" || got[1] != "forwards" {
		t.Fatalf("%v", got)
	}

	own := domain.Data{Hosts: []domain.Host{{Label: "own"}}, Groups: []domain.Group{{Name: "g"}}, Identities: []domain.Identity{{Name: "k"}}, Forwards: []domain.PortForward{{}}, Snippets: []domain.Snippet{{Label: "s"}}}
	ownSecrets := []domain.Secret{{}}
	sent, sentSecrets := selectData(own, ownSecrets, []string{syncSnippets})
	if len(sent.Hosts)+len(sent.Groups)+len(sent.Identities)+len(sent.Forwards) != 0 || len(sentSecrets) != 0 || len(sent.Snippets) != 1 {
		t.Fatalf("only snippets go out: %+v %v", sent, sentSecrets)
	}
	merged := domain.Data{Hosts: []domain.Host{{Label: "remote"}, {Label: "own"}}, Snippets: []domain.Snippet{{Label: "s"}, {Label: "remote-s"}}}
	kept, keptSecrets := keepUnticked(merged, own, []domain.Secret{{}, {}}, ownSecrets, []string{syncSnippets})
	if len(kept.Hosts) != 1 || kept.Hosts[0].Label != "own" || len(kept.Snippets) != 2 || len(keptSecrets) != 1 || len(kept.Identities) != 1 {
		t.Fatalf("unticked kinds stay as they were, ticked ones are merged: %+v %v", kept, keptSecrets)
	}
}
