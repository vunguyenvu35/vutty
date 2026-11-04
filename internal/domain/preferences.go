package domain

import (
	"encoding/json"
	"time"
)

// Preferences are the settings that follow the user from one machine to the next, in groups the user can choose between
// (appearance, terminal, language, shortcuts, behaviour, account). It is an allow-list on purpose: a setting that belongs to
// one machine (zoom, the folders for sync and session logs, auto-lock, Touch ID, the graphics renderer) is in no group.
// Each group has its own date, so that a machine that syncs only some groups cannot overwrite the others, and the newest
// copy of each group wins. The values of a group are JSON whose shape belongs to the use case that reads it.
type Preferences map[string]PrefGroup

// PrefGroup is one group: when one of its values was last changed on some machine, and the values.
type PrefGroup struct {
	UpdatedAt time.Time       `json:"updated_at"`
	Values    json.RawMessage `json:"values"`
}

// mergePrefs keeps, for every group, the newer copy; a group only one side has is kept, and on a tie the local one stays.
func mergePrefs(local, remote Preferences) Preferences {
	if len(local) == 0 && len(remote) == 0 {
		return nil
	}
	out := make(Preferences, len(local)+len(remote))
	for id, g := range local {
		out[id] = g
	}
	for id, r := range remote {
		if l, ok := out[id]; !ok || r.UpdatedAt.After(l.UpdatedAt) {
			out[id] = r
		}
	}
	return out
}
