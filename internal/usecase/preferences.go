package usecase

import (
	"bytes"
	"encoding/json"
	"slices"
	"time"

	"vutty/internal/domain"
)

// A preference group is a set of settings the user can choose to sync (Settings → Sync). Its id is stored in
// settings.json (SyncWhat) and in the bundle, so ids never change.
type prefGroup struct {
	id  string
	get func(Settings) any
	set func(*Settings, json.RawMessage) error
}

type (
	appearancePrefs struct {
		Theme         string `json:"theme"`
		TerminalTheme string `json:"terminal_theme"`
	}
	terminalPrefs struct {
		FontSize    int    `json:"font_size"`
		FontFamily  string `json:"font_family"`
		CursorStyle string `json:"cursor_style"`
		CursorBlink bool   `json:"cursor_blink"`
		Scrollback  int    `json:"scrollback"`
	}
	languagePrefs struct {
		Language string `json:"language"`
	}
	shortcutPrefs struct {
		Shortcuts map[string]string `json:"shortcuts"`
	}
	behaviourPrefs struct {
		Highlight bool `json:"highlight"`
		Suggest   bool `json:"suggest"`
	}
	accountPrefs struct {
		Name  string `json:"account_name"`
		Email string `json:"account_email"`
	}
)

var prefGroups = []prefGroup{
	{"appearance",
		func(s Settings) any { return appearancePrefs{s.Theme, s.TerminalTheme} },
		func(s *Settings, raw json.RawMessage) error {
			var v appearancePrefs
			if err := json.Unmarshal(raw, &v); err != nil {
				return err
			}
			s.Theme, s.TerminalTheme = v.Theme, v.TerminalTheme
			return nil
		}},
	{"terminal",
		func(s Settings) any {
			return terminalPrefs{s.FontSize, s.FontFamily, s.CursorStyle, s.CursorBlink, s.Scrollback}
		},
		func(s *Settings, raw json.RawMessage) error {
			var v terminalPrefs
			if err := json.Unmarshal(raw, &v); err != nil {
				return err
			}
			s.FontSize, s.CursorStyle, s.CursorBlink, s.Scrollback = v.FontSize, v.CursorStyle, v.CursorBlink, v.Scrollback
			if v.FontFamily != "" {
				s.FontFamily = v.FontFamily
			}
			return nil
		}},
	{"language",
		func(s Settings) any { return languagePrefs{s.Language} },
		func(s *Settings, raw json.RawMessage) error {
			var v languagePrefs
			if err := json.Unmarshal(raw, &v); err != nil {
				return err
			}
			s.Language = v.Language
			if s.Language == "" {
				s.Language = "auto"
			}
			return nil
		}},
	{"shortcuts",
		func(s Settings) any { return shortcutPrefs{cloneShortcuts(s.Shortcuts)} },
		func(s *Settings, raw json.RawMessage) error {
			var v shortcutPrefs
			if err := json.Unmarshal(raw, &v); err != nil {
				return err
			}
			s.Shortcuts = cloneShortcuts(v.Shortcuts)
			return nil
		}},
	{"behaviour",
		func(s Settings) any { return behaviourPrefs{s.Highlight, s.Suggest} },
		func(s *Settings, raw json.RawMessage) error {
			var v behaviourPrefs
			if err := json.Unmarshal(raw, &v); err != nil {
				return err
			}
			s.Highlight, s.Suggest = v.Highlight, v.Suggest
			return nil
		}},
	{"account",
		func(s Settings) any { return accountPrefs{s.AccountName, s.AccountEmail} },
		func(s *Settings, raw json.RawMessage) error {
			var v accountPrefs
			if err := json.Unmarshal(raw, &v); err != nil {
				return err
			}
			s.AccountName, s.AccountEmail = v.Name, v.Email
			return nil
		}},
}

// The kinds of data the user can choose to sync (Settings → Sync → What to sync), besides the preference groups.
// Their ids are stored in settings.json (SyncWhat), so they never change.
const (
	syncHosts    = "hosts"    // hosts and groups
	syncKeys     = "keys"     // saved keys, passwords and ssh-agent identities (with the secrets of Telnet hosts)
	syncForwards = "forwards" // port forwards; they point at hosts, so they need Hosts
	syncSnippets = "snippets" // snippets
)

var dataKinds = []string{syncHosts, syncKeys, syncForwards, syncSnippets}

// allSyncWhat is every id the user can tick, and the default of SyncWhat: the data first, then the preference groups.
func allSyncWhat() []string {
	ids := append([]string{}, dataKinds...)
	for _, g := range prefGroups {
		ids = append(ids, g.id)
	}
	return ids
}

// cleanSyncWhat keeps the known ids of a list, once each and in the fixed order. Port forwards without hosts make no
// sense (a forward points at a host), so Forwards is dropped when Hosts is not ticked.
func cleanSyncWhat(in []string) []string {
	out := []string{}
	for _, id := range allSyncWhat() {
		if !slices.Contains(in, id) || (id == syncForwards && !slices.Contains(in, syncHosts)) {
			continue
		}
		out = append(out, id)
	}
	return out
}

func (g prefGroup) values(s Settings) json.RawMessage {
	b, _ := json.Marshal(g.get(s)) // encoding/json writes map keys in order: equal values give equal bytes
	return b
}

// withPrefDate returns the dates of the groups with one changed. It copies the map: a Settings value is handed out by Get
// and read elsewhere while this runs, so the map in it is never written to.
func withPrefDate(m map[string]time.Time, id string, at time.Time) map[string]time.Time {
	out := make(map[string]time.Time, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	out[id] = at
	return out
}

// stampPrefs dates the groups whose values differ between before and the current settings (call with setMu held).
func (c *Core) stampPrefs(before Settings) {
	now := time.Now().UTC()
	for _, g := range prefGroups {
		if !bytes.Equal(g.values(before), g.values(c.settings)) {
			c.settings.PrefsUpdated = withPrefDate(c.settings.PrefsUpdated, g.id, now)
		}
	}
}

// outgoingPrefs is what this machine puts in the bundle: the groups the user ticked, and of those only the ones that were
// ever changed (a new machine that kept every default must not outvote the one that holds the real values). A machine set up
// before this existed has no dates: a group that differs from the default counts as changed now.
func (c *Core) outgoingPrefs() domain.Preferences {
	c.setMu.Lock()
	defer c.setMu.Unlock()
	def := defaultSettings()
	out := domain.Preferences{}
	dirty := false
	for _, g := range prefGroups {
		if !slices.Contains(c.settings.SyncWhat, g.id) {
			continue
		}
		at := c.settings.PrefsUpdated[g.id]
		if at.IsZero() {
			if bytes.Equal(g.values(c.settings), g.values(def)) {
				continue
			}
			at = time.Now().UTC()
			c.settings.PrefsUpdated = withPrefDate(c.settings.PrefsUpdated, g.id, at)
			dirty = true
		}
		out[g.id] = domain.PrefGroup{UpdatedAt: at, Values: g.values(c.settings)}
	}
	if dirty {
		_ = c.saveSettings()
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyPrefs takes, for every ticked group, the copy of a synced bundle when it is newer than the one here and valid. It never
// fails a sync: a group that does not pass the checks of Save is left alone.
func (c *Core) applyPrefs(p domain.Preferences) {
	if len(p) == 0 {
		return
	}
	c.setMu.Lock()
	changed := false
	for _, g := range prefGroups {
		r, ok := p[g.id]
		if !ok || r.UpdatedAt.IsZero() || !slices.Contains(c.settings.SyncWhat, g.id) || !r.UpdatedAt.After(c.settings.PrefsUpdated[g.id]) {
			continue
		}
		next := c.settings
		if g.set(&next, r.Values) != nil || validateSettings(next) != nil {
			continue
		}
		next.PrefsUpdated = withPrefDate(c.settings.PrefsUpdated, g.id, r.UpdatedAt)
		prev := c.settings
		c.settings = next
		if err := c.saveSettings(); err != nil {
			c.settings = prev
			continue
		}
		changed = true
	}
	c.setMu.Unlock()
	if changed {
		c.emit("settings:changed")
	}
}
