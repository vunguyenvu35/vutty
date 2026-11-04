package usecase

import (
	"slices"

	"vutty/internal/domain"
)

// syncWhat is a copy of what the user ticked.
func (c *Core) syncWhat() []string {
	c.setMu.Lock()
	defer c.setMu.Unlock()
	return slices.Clone(c.settings.SyncWhat)
}

// selectData is what this machine sends: the kinds of data that are ticked. An unticked kind is left out of the local
// bundle, so the merge keeps what the repository holds for it and nothing is sent.
func selectData(d domain.Data, secrets []domain.Secret, what []string) (domain.Data, []domain.Secret) {
	if !slices.Contains(what, syncHosts) {
		d.Groups, d.Hosts = nil, nil
	}
	if !slices.Contains(what, syncKeys) {
		d.Identities, secrets = nil, nil
	}
	if !slices.Contains(what, syncForwards) {
		d.Forwards = nil
	}
	if !slices.Contains(what, syncSnippets) {
		d.Snippets = nil
	}
	return d, secrets
}

// keepUnticked is what this machine keeps after a sync: the merged data for the kinds that are ticked, and for an unticked kind
// what it already had (the repository's copy of it is not brought here).
func keepUnticked(merged, own domain.Data, mergedSecrets, ownSecrets []domain.Secret, what []string) (domain.Data, []domain.Secret) {
	if !slices.Contains(what, syncHosts) {
		merged.Groups, merged.Hosts = own.Groups, own.Hosts
	}
	if !slices.Contains(what, syncKeys) {
		merged.Identities, mergedSecrets = own.Identities, ownSecrets
	}
	if !slices.Contains(what, syncForwards) {
		merged.Forwards = own.Forwards
	}
	if !slices.Contains(what, syncSnippets) {
		merged.Snippets = own.Snippets
	}
	return merged, mergedSecrets
}
