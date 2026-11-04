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

// secretsOf keeps the secrets of one side: the kubeconfigs of the clusters, or all the others (keys, passwords, Telnet logins). They
// are ticked apart: a cluster is useless without its kubeconfig, and the kubeconfig has no use apart from its cluster.
func secretsOf(secrets []domain.Secret, kubeconfigs bool) []domain.Secret {
	var out []domain.Secret
	for _, s := range secrets {
		if (s.Kind == domain.SecretKubeconfig) == kubeconfigs {
			out = append(out, s)
		}
	}
	return out
}

// selectData is what this machine sends: the kinds of data that are ticked. An unticked kind is left out of the local
// bundle, so the merge keeps what the repository holds for it and nothing is sent.
func selectData(d domain.Data, secrets []domain.Secret, what []string) (domain.Data, []domain.Secret) {
	if !slices.Contains(what, syncHosts) {
		d.Groups, d.Hosts = nil, nil
	}
	var out []domain.Secret
	if slices.Contains(what, syncKeys) {
		out = append(out, secretsOf(secrets, false)...)
	} else {
		d.Identities = nil
	}
	if slices.Contains(what, syncClusters) {
		out = append(out, secretsOf(secrets, true)...)
	} else {
		d.Clusters = nil
	}
	if !slices.Contains(what, syncForwards) {
		d.Forwards = nil
	}
	if !slices.Contains(what, syncSnippets) {
		d.Snippets = nil
	}
	return d, out
}

// keepUnticked is what this machine keeps after a sync: the merged data for the kinds that are ticked, and for an unticked kind
// what it already had (the repository's copy of it is not brought here).
func keepUnticked(merged, own domain.Data, mergedSecrets, ownSecrets []domain.Secret, what []string) (domain.Data, []domain.Secret) {
	if !slices.Contains(what, syncHosts) {
		merged.Groups, merged.Hosts = own.Groups, own.Hosts
	}
	keys, clusters := mergedSecrets, mergedSecrets
	if !slices.Contains(what, syncKeys) {
		merged.Identities, keys = own.Identities, ownSecrets
	}
	if !slices.Contains(what, syncClusters) {
		merged.Clusters, clusters = own.Clusters, ownSecrets
	}
	mergedSecrets = append(secretsOf(keys, false), secretsOf(clusters, true)...)
	if !slices.Contains(what, syncForwards) {
		merged.Forwards = own.Forwards
	}
	if !slices.Contains(what, syncSnippets) {
		merged.Snippets = own.Snippets
	}
	return merged, mergedSecrets
}
