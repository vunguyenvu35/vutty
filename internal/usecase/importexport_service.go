package usecase

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"vutty/internal/fsutil"

	"vutty/internal/domain"
	"vutty/internal/exporter"
	"vutty/internal/importer"
	"vutty/internal/syncgit"
)

// ImportReport summarises an import for the UI.
type ImportReport struct {
	HostsAdded    int      `json:"hosts_added"`
	GroupsAdded   int      `json:"groups_added"`
	ForwardsAdded int      `json:"forwards_added"`
	KeysImported  int      `json:"keys_imported"`
	Skipped       []string `json:"skipped"`
	Warnings      []string `json:"warnings"`
	// Format is what an inventory import took the file to be.
	Format string `json:"format,omitempty"`
}

// ExportReport summarises an ssh config export.
type ExportReport struct {
	Files    []string `json:"files"`
	KeyFiles []string `json:"key_files"`
	Warnings []string `json:"warnings"`
}

// ImportExportService converts between Vutty data and standard formats.
type ImportExportService struct{ c *Core }

func NewImportExportService(c *Core) *ImportExportService { return &ImportExportService{c} }

// ImportSSHConfig reads an OpenSSH config (following Include). conflict is "skip",
// "overwrite" or "duplicate" for hosts whose label already exists.
func (s *ImportExportService) ImportSSHConfig(path, conflict string) (ImportReport, error) {
	var rep ImportReport
	policy, err := conflictPolicy(conflict)
	if err != nil {
		return rep, err
	}
	parsed, err := importer.ParseFile(path)
	if err != nil {
		return rep, err
	}
	return s.applyPlan(importer.Build(parsed, s.c.Store.Snapshot(), policy), rep)
}

// maxInventoryFile bounds a listing that is read: it is a text file somebody exported, not a database.
const maxInventoryFile = 16 << 20

// InventoryImportOptions are what a listing of servers does not say (see importer.InventoryOptions).
type InventoryImportOptions struct {
	User           string `json:"user"`
	Address        string `json:"address"` // "" or "public", or "private"
	Group          string `json:"group"`
	IncludeStopped bool   `json:"include_stopped"`
}

// ImportInventory reads a listing of servers that you made with your own tools (aws ec2 describe-instances, gcloud compute
// instances list --format=json, ansible-inventory --list or an Ansible INI file, or a CSV) and adds its hosts. Vutty never
// contacts a cloud and never sees its credentials. conflict is as for ImportSSHConfig. The report says which format was read.
func (s *ImportExportService) ImportInventory(path, conflict string, opts InventoryImportOptions) (ImportReport, error) {
	var rep ImportReport
	policy, err := conflictPolicy(conflict)
	if err != nil {
		return rep, err
	}
	switch opts.Address {
	case "", "public", "private":
	default:
		return rep, fmt.Errorf("address must be public or private")
	}
	if len(opts.User) > 64 || strings.ContainsAny(opts.User, " \t\r\n\x00") || len(opts.Group) > 80 || strings.ContainsAny(opts.Group, "\r\n\x00") {
		return rep, errors.New("the user name or the group is not valid")
	}
	f, err := os.Open(path)
	if err != nil {
		return rep, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxInventoryFile+1))
	if err != nil {
		return rep, err
	}
	if len(data) > maxInventoryFile {
		return rep, fmt.Errorf("the file is larger than %d MB", maxInventoryFile>>20)
	}
	parsed, format, err := importer.ParseInventory(data, importer.InventoryOptions{User: opts.User, Address: opts.Address, Group: opts.Group, IncludeStopped: opts.IncludeStopped})
	if err != nil {
		return rep, err
	}
	rep.Format = format
	return s.applyPlan(importer.Build(parsed, s.c.Store.Snapshot(), policy), rep)
}

// conflictPolicy reads the "what to do when a host with the same name exists" choice of the import screens.
func conflictPolicy(conflict string) (importer.Policy, error) {
	switch conflict {
	case "", "skip":
		return importer.PolicySkip, nil
	case "overwrite":
		return importer.PolicyOverwrite, nil
	case "duplicate":
		return importer.PolicyDuplicate, nil
	}
	return "", fmt.Errorf("unknown conflict policy %q", conflict)
}

// applyPlan stores what an import planned: the keys first (so hosts can use them), then groups, hosts and forwards.
func (s *ImportExportService) applyPlan(plan importer.Plan, rep ImportReport) (ImportReport, error) {
	rep.Skipped = nonNil(plan.Skipped) // JSON must carry [] not null: the UI calls .length
	rep.Warnings = nonNil(plan.Warnings)

	// keys first, so hosts can reference the new identities
	identByPath := map[string]string{}
	hostIdent := map[string]string{}
	unlocked := s.c.Vault.Status().Unlocked
	if len(plan.KeyFiles) > 0 && !unlocked {
		rep.Warnings = append(rep.Warnings, "vault is locked: private keys referenced by IdentityFile were not imported")
	}
	for hid, p := range plan.KeyFiles {
		if !unlocked {
			break
		}
		if id, ok := identByPath[p]; ok {
			hostIdent[hid] = id
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("key %s: %v", p, err))
			continue
		}
		id, err := s.c.importKey(filepath.Base(p), b, nil)
		if err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("key %s not imported: %v", p, err))
			continue
		}
		identByPath[p] = id.ID
		hostIdent[hid] = id.ID
		rep.KeysImported++
	}

	snap := s.c.Store.Snapshot()
	for _, g := range plan.Groups {
		g.UpdatedAt = nowUTC()
		snap.Groups = append(snap.Groups, g)
		rep.GroupsAdded++
	}
	byID := map[string]int{}
	for i, h := range snap.Hosts {
		byID[h.ID] = i
	}
	for _, h := range plan.Hosts {
		h.IdentityID = firstNonEmpty(hostIdent[h.ID], h.IdentityID)
		h.UpdatedAt = nowUTC()
		h.CreatedAt = h.UpdatedAt
		h.KeepaliveSec = 30
		if i, ok := byID[h.ID]; ok {
			h.CreatedAt = snap.Hosts[i].CreatedAt
			if h.IdentityID == "" {
				h.IdentityID = snap.Hosts[i].IdentityID
			}
			snap.Hosts[i] = h
		} else {
			snap.Hosts = append(snap.Hosts, h)
		}
		rep.HostsAdded++
	}
	for _, f := range plan.Forwards {
		f.UpdatedAt = nowUTC()
		snap.Forwards = append(snap.Forwards, f)
		rep.ForwardsAdded++
	}
	if err := s.c.Store.Replace(snap); err != nil {
		return rep, err
	}
	return rep, nil
}

// ImportBackup restores everything from an encrypted backup. Existing records are merged
// by id (newer updated_at wins); secrets are re-encrypted into the current vault.
func (s *ImportExportService) ImportBackup(path, password string) error {
	if !s.c.Vault.Status().Unlocked {
		return errNotUnlocked
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	b, err := exporter.DecryptBackup(password, blob)
	if err != nil {
		return err
	}
	for _, sec := range b.Secrets {
		if err := s.c.Vault.PutSecret(sec.ID, sec.Kind, sec.Value); err != nil {
			return err
		}
	}
	cur := s.c.Store.Snapshot()
	return s.c.Store.Replace(mergeData(cur, b.Data))
}

// ExportSSHConfig writes ssh config files into dir. Private keys are only exported when
// withKeys is true (and the vault is unlocked); they get mode 0600.
func (s *ImportExportService) ExportSSHConfig(dir string, withKeys bool) (ExportReport, error) {
	if dir == "" {
		return ExportReport{}, errors.New("target directory is required")
	}
	if withKeys && !s.c.Vault.Status().Unlocked {
		return ExportReport{}, errNotUnlocked
	}
	keys := func(identityID string) ([]byte, error) {
		x, err := s.c.Store.GetIdentity(identityID)
		if err != nil {
			return nil, err
		}
		return s.c.Vault.GetSecret(x.SecretRef)
	}
	r, err := exporter.SSHConfig(dir, s.c.Store.Snapshot(), withKeys, keys)
	return ExportReport{Files: nonNil(r.Files), KeyFiles: nonNil(r.KeyFiles), Warnings: nonNil(r.Warnings)}, err
}

// ExportPlainJSON writes metadata only (no secrets) for sharing a host list.
func (s *ImportExportService) ExportPlainJSON(path string) error {
	b, err := exporter.PlainJSON(s.c.Store.Snapshot())
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, b, 0o600)
}

// ExportBackup writes everything, secrets included, encrypted with its own password.
func (s *ImportExportService) ExportBackup(path, password string) error {
	secrets, err := s.c.Vault.DumpSecrets()
	if err != nil {
		return vaultErr(err)
	}
	blob, err := exporter.EncryptBackup(password, s.c.Store.Snapshot(), secrets)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, blob, 0o600)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// mergeData applies backup records on top of current data by id, newest updated_at wins.
func mergeData(cur, in domain.Data) domain.Data {
	return syncgit.Merge(syncgit.Bundle{Data: cur}, syncgit.Bundle{Data: in}).Data
}

func nowUTC() time.Time { return time.Now().UTC() }

// nonNil turns a nil slice into an empty one so it marshals to [] instead of null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
