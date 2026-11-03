package usecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vutty/internal/domain"
	"vutty/internal/syncfolder"
	"vutty/internal/syncgit"
	"vutty/internal/vault"
)

// SyncStatus is the frontend view of the sync state.
type SyncStatus struct {
	Configured bool      `json:"configured"`
	RepoURL    string    `json:"repo_url"`
	Folder     string    `json:"folder"` // set in synced-folder mode; RepoURL is empty then
	LastSync   time.Time `json:"last_sync"`
	LastError  string    `json:"last_error,omitempty"`
	Syncing    bool      `json:"syncing"`
	// LocalEmpty is true while this machine holds nothing of its own (no host, group, key, snippet or forward): the
	// vault of a sync repository can then simply be taken over.
	LocalEmpty bool `json:"local_empty"`
}

// SyncService syncs the encrypted bundle through a Git repo the user owns, or through a folder
// that a cloud client (Google Drive, iCloud Drive, Dropbox...) keeps in step.
type SyncService struct {
	c *Core

	mu     sync.Mutex
	status SyncStatus
}

func NewSyncService(c *Core) *SyncService {
	s := &SyncService{c: c}
	s.status.RepoURL = c.settings.SyncURL
	s.status.Folder = c.settings.SyncFolder
	s.status.Configured = c.settings.SyncURL != "" || c.settings.SyncFolder != ""
	return s
}

func (s *SyncService) repo() *syncgit.Repo {
	return &syncgit.Repo{Dir: s.c.SyncDir, URL: s.status.RepoURL}
}

// Configure stores the repo URL and prepares the local working copy. Authentication relies on
// the user's ssh-agent or git credential helper.
func (s *SyncService) Configure(repoURL string) error {
	repoURL = strings.TrimSpace(repoURL)
	r := &syncgit.Repo{Dir: s.c.SyncDir, URL: repoURL}
	if err := r.Init(); err != nil {
		return err
	}
	s.c.setMu.Lock()
	s.c.settings.SyncURL, s.c.settings.SyncFolder = repoURL, ""
	err := s.c.saveSettings()
	s.c.setMu.Unlock()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.status.RepoURL, s.status.Folder, s.status.Configured, s.status.LastError = repoURL, "", true, ""
	s.mu.Unlock()
	return nil
}

// ConfigureFolder syncs through a folder instead of Git. Only bundle.enc and vault-header.json
// are written there, both encrypted.
func (s *SyncService) ConfigureFolder(dir string) error {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if err := (&syncfolder.Folder{Dir: dir}).Check(); err != nil {
		return err
	}
	s.c.setMu.Lock()
	s.c.settings.SyncFolder, s.c.settings.SyncURL = dir, ""
	err := s.c.saveSettings()
	s.c.setMu.Unlock()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.status.Folder, s.status.RepoURL, s.status.Configured, s.status.LastError = dir, "", true, ""
	s.mu.Unlock()
	return nil
}

func (s *SyncService) folder() (*syncfolder.Folder, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.Folder == "" {
		return nil, false
	}
	return &syncfolder.Folder{Dir: s.status.Folder}, true
}

func (s *SyncService) Status() SyncStatus {
	s.mu.Lock()
	st := s.status
	s.mu.Unlock()
	st.LocalEmpty = s.localEmpty()
	return st
}

// localEmpty reports whether this machine has no data of its own, so that nothing is lost by taking over another vault.
func (s *SyncService) localEmpty() bool {
	if !s.c.Vault.Status().Exists {
		return false
	}
	d := s.c.Store.Snapshot()
	for _, h := range d.Hosts {
		if !h.Deleted() {
			return false
		}
	}
	for _, g := range d.Groups {
		if !g.Deleted() {
			return false
		}
	}
	for _, i := range d.Identities {
		if !i.Deleted() {
			return false
		}
	}
	for _, x := range d.Snippets {
		if !x.Deleted() {
			return false
		}
	}
	for _, f := range d.Forwards {
		if !f.Deleted() {
			return false
		}
	}
	for _, x := range s.c.Vault.Secrets() {
		if !x.Deleted() {
			return false
		}
	}
	return true
}

// errWrongKey stops a sync from sending a bundle that nobody could open with the master password.
var errWrongKey = errors.New("this vault is open with a key that does not belong to it (one left over from another vault), so nothing was sent: lock the vault, unlock it with the master password and try again")

func (s *SyncService) begin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.status.Configured {
		return errors.New("sync is not configured: choose a Git repository or a folder first")
	}
	if s.status.Syncing {
		return errors.New("a sync is already running")
	}
	s.status.Syncing = true
	return nil
}

func (s *SyncService) end(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Syncing = false
	if err != nil {
		s.status.LastError = err.Error()
		s.c.record(domain.ActSync, "", "", "Sync failed: "+err.Error(), false)
		return
	}
	s.c.record(domain.ActSync, "", "", "Synced", true)
	s.status.LastError = ""
	s.status.LastSync = time.Now().UTC()
}

// SyncNow fetches, merges per record, re-encrypts and pushes. The vault must be unlocked.
func (s *SyncService) SyncNow() (err error) {
	if err = s.begin(); err != nil {
		return err
	}
	defer func() { s.end(err) }()
	return s.doSync()
}

// doSync is SyncNow once the run has begun (SyncNow and JoinRemoteVault share it).
func (s *SyncService) doSync() error {
	if !s.c.Vault.Status().Unlocked {
		return errNotUnlocked
	}
	if err := s.c.Vault.CheckKey(); err != nil {
		return errWrongKey
	}
	hdr, err := s.c.Vault.Header()
	if err != nil {
		return err
	}
	local := syncgit.Bundle{Data: s.c.Store.Snapshot(), Secrets: s.c.Vault.Secrets()}
	var res syncgit.Result
	if f, ok := s.folder(); ok {
		res, err = f.Sync(local, hdr, s.c.Vault)
	} else {
		r := s.repo()
		if err = r.Init(); err != nil {
			return err
		}
		res, err = r.Sync(local, hdr, s.c.Vault)
	}
	if err != nil {
		return err
	}
	if err = s.c.Store.Replace(res.Merged.Data); err != nil {
		return err
	}
	return s.c.Vault.ReplaceSecrets(res.Merged.Secrets)
}

// remote reads the header and the encrypted bundle that the repository or folder holds (nothing is changed).
func (s *SyncService) remote() (hdr vault.Header, enc []byte, exists bool, err error) {
	if f, ok := s.folder(); ok {
		if err = f.Check(); err != nil {
			return
		}
		return f.Remote()
	}
	r := s.repo()
	if err = r.Init(); err != nil {
		return
	}
	if err = r.Fetch(); err != nil {
		return
	}
	return r.Remote()
}

// JoinRemoteVault is for a machine that already has a vault of its own while the repository (or folder) holds the
// bundle of another one: their keys differ, so a plain sync stops with "encrypted with a different vault key".
// With the master password of the synced vault this machine takes that vault's key and master password, keeps its
// own hosts and saved keys (sealed again under the new key), merges them with what the repository holds and syncs.
// The vault as it was is first saved in <data>/backups, so nothing is lost if this was not what the user wanted.
func (s *SyncService) JoinRemoteVault(masterPassword string) (err error) {
	if err = s.begin(); err != nil {
		return err
	}
	defer func() { s.end(err) }()
	if !s.c.Vault.Status().Unlocked {
		return errNotUnlocked
	}
	hdr, enc, exists, err := s.remote()
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("nothing to join: no bundle was found there yet, use Sync now")
	}
	// the password must open the repository's bundle before anything here is touched
	if _, err = vault.OpenWithHeader(masterPassword, hdr, enc, []byte(syncgit.BundleAAD)); err != nil {
		if errors.Is(err, vault.ErrBadPassword) {
			return errors.New("that is not the master password of the synced vault")
		}
		return errors.New("the password is right, but this repository's bundle was not locked with that vault's key, so nobody can open it: use Replace below to make the repository hold this Mac's data")
	}
	if err = s.backupBeforeJoin(); err != nil {
		return fmt.Errorf("could not save a copy of this vault first, nothing was changed: %w", err)
	}
	if err = s.c.Vault.AdoptKey(masterPassword, hdr); err != nil {
		return err
	}
	return s.doSync()
}

// ReplaceRemote makes the repository (or folder) hold this vault's data and nothing else. It is for a repository whose
// bundle this vault cannot open and whose own vault is out of reach (its password is lost, or the bundle was written
// with a key that does not match its header). It refuses when this vault can read the repository (use Sync now then),
// and the files that were there are first copied to <data>/backups, so they can be put back.
func (s *SyncService) ReplaceRemote() (err error) {
	if err = s.begin(); err != nil {
		return err
	}
	defer func() { s.end(err) }()
	if !s.c.Vault.Status().Unlocked {
		return errNotUnlocked
	}
	if err := s.c.Vault.CheckKey(); err != nil {
		return errWrongKey
	}
	hdr, err := s.c.Vault.Header()
	if err != nil {
		return err
	}
	rhdr, enc, exists, err := s.remote()
	if err != nil {
		return err
	}
	if exists {
		if _, e := s.c.Vault.OpenBlob(enc, []byte(syncgit.BundleAAD)); e == nil {
			return errors.New("this vault can read the repository: use Sync now, nothing needs replacing")
		}
		if err = s.backupRemote(rhdr, enc); err != nil {
			return fmt.Errorf("could not save a copy of what the repository holds first, nothing was changed: %w", err)
		}
	}
	local := syncgit.Bundle{Data: s.c.Store.Snapshot(), Secrets: s.c.Vault.Secrets()}
	if f, ok := s.folder(); ok {
		return f.Replace(local, hdr, s.c.Vault)
	}
	r := s.repo()
	if err = r.Init(); err != nil {
		return err
	}
	return r.Replace(local, hdr, s.c.Vault)
}

// backupRemote copies the header and the bundle of the repository into <data>/backups before they are replaced.
func (s *SyncService) backupRemote(hdr vault.Header, enc []byte) error {
	dir := filepath.Join(s.c.Dir, "backups", "replaced-remote-"+time.Now().UTC().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	hb, err := json.MarshalIndent(hdr, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, syncgit.HeaderFile), hb, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, syncgit.BundleFile), enc, 0o600)
}

// backupBeforeJoin writes the vault file (still under its own key and password) and the data as they are now.
func (s *SyncService) backupBeforeJoin() error {
	hdr, err := s.c.Vault.Header()
	if err != nil {
		return err
	}
	dir := filepath.Join(s.c.Dir, "backups", "before-joining-"+time.Now().UTC().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	files := map[string]any{
		"vault-before-joining.json": vault.Snapshot{Header: hdr, Secrets: s.c.Vault.Secrets()},
		"data-before-joining.json":  s.c.Store.Snapshot(),
	}
	for name, v := range files {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// JoinExisting sets up a new machine from a repo that already holds a bundle: the master
// password unwraps the DEK from vault-header.json and all data is restored.
func (s *SyncService) JoinExisting(masterPassword string) (err error) {
	if err = s.begin(); err != nil {
		return err
	}
	defer func() { s.end(err) }()
	if s.c.Vault.Status().Exists {
		return errors.New("this machine already has a vault; use Sync now instead")
	}
	var (
		hdr    vault.Header
		enc    []byte
		exists bool
	)
	if f, ok := s.folder(); ok {
		if err = f.Check(); err != nil {
			return err
		}
		hdr, enc, exists, err = f.Remote()
	} else {
		r := s.repo()
		if err = r.Init(); err != nil {
			return err
		}
		if err = r.Fetch(); err != nil {
			return err
		}
		hdr, enc, exists, err = r.Remote()
	}
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("nothing to restore: no Vutty bundle was found there yet")
	}
	if err = s.c.Vault.Adopt(masterPassword, hdr, nil); err != nil {
		return err
	}
	plain, err := s.c.Vault.OpenBlob(enc, []byte(syncgit.BundleAAD))
	if err != nil {
		return fmt.Errorf("bundle cannot be decrypted: %w", err)
	}
	var b syncgit.Bundle
	if err = json.Unmarshal(plain, &b); err != nil {
		return err
	}
	if err = s.c.Store.Replace(b.Data); err != nil {
		return err
	}
	return s.c.Vault.ReplaceSecrets(b.Secrets)
}
