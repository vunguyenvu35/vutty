package usecase

import (
	"os/exec"
	"strings"
	"testing"

	"vutty/internal/domain"
	"vutty/internal/vault"
)

func TestHealthFindsTheSecretsThatTheKeyCannotOpenAndNamesTheirOwners(t *testing.T) {
	h := newHarness(t).unlocked(t)
	good, _ := h.app.Vault.AddPassword("fine", "root", "ok")
	broken, _ := h.app.Vault.AddPassword("broken", "root", "will-not-open")

	if r, err := h.app.Vault.Health(); err != nil || r.Checked != 2 || len(r.Unreadable) != 0 {
		t.Fatalf("a healthy vault: %+v %v", r, err)
	}

	// the secret of "broken" is replaced by one that another vault sealed under the same id: what a stale key leaves behind
	other := newHarness(t).unlocked(t)
	if err := other.core.Vault.PutSecret(broken.SecretRef, domain.SecretPassword, []byte("sealed elsewhere")); err != nil {
		t.Fatal(err)
	}
	var foreign domain.Secret
	for _, s := range other.core.Vault.Secrets() {
		if s.ID == broken.SecretRef {
			foreign = s
		}
	}
	var next []domain.Secret
	for _, s := range h.core.Vault.Secrets() {
		if s.ID != broken.SecretRef {
			next = append(next, s)
		}
	}
	if err := h.core.Vault.ReplaceSecrets(append(next, foreign)); err != nil {
		t.Fatal(err)
	}

	r, err := h.app.Vault.Health()
	if err != nil {
		t.Fatal(err)
	}
	if r.Checked != 2 || len(r.Unreadable) != 1 {
		t.Fatalf("%+v", r)
	}
	u := r.Unreadable[0]
	if u.Name != "broken" || u.IdentityID != broken.ID || u.Kind != "password" || u.SecretID != broken.SecretRef {
		t.Fatalf("%+v", u)
	}
	if u.IdentityID == good.ID {
		t.Fatal("the healthy one was reported")
	}

	// a host that uses it says what to do, instead of "message authentication failed"
	host, _ := h.app.Host.SaveHost(modelHost("srv", 22, broken.ID))
	if _, err := h.core.hopFor(host); err == nil || !strings.Contains(err.Error(), "enter it again") || strings.Contains(err.Error(), "cipher") {
		t.Fatalf("want the advice, got %v", err)
	}

	// editing the owner puts a secret the key can read in its place
	if _, err := h.app.Vault.UpdateIdentity(broken.ID, "broken", "root", "typed again", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if r, _ := h.app.Vault.Health(); len(r.Unreadable) != 0 {
		t.Fatalf("still unreadable after entering it again: %+v", r)
	}
}

func TestHealthNeedsAnUnlockedVault(t *testing.T) {
	h := newHarness(t).unlocked(t)
	h.app.Vault.Lock()
	if _, err := h.app.Vault.Health(); err == nil {
		t.Fatal("a locked vault was checked")
	}
}

// wrongKey is a vault whose key is not the one of its header.
type wrongKey struct{ SecretVault }

func (wrongKey) CheckKey() error { return vault.ErrKeyMismatch }

func TestSyncSendsNothingWhenTheVaultKeyIsNotTheHeadersKey(t *testing.T) {
	remote := bareRepo(t)
	h := newHarness(t).unlocked(t)
	h.app.Host.SaveHost(modelHost("a", 22, ""))
	if err := h.app.Sync.Configure(remote); err != nil {
		t.Fatal(err)
	}
	real := h.core.Vault
	h.core.Vault = wrongKey{real}
	err := h.app.Sync.SyncNow()
	if err == nil || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("want the reason, got %v", err)
	}
	if out, _ := exec.Command("git", "-C", remote, "for-each-ref").Output(); len(out) != 0 {
		t.Fatalf("something reached the repository: %s", out)
	}
	if err := h.app.Sync.ReplaceRemote(); err == nil || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("replace must refuse too: %v", err)
	}
	h.core.Vault = real
	if err := h.app.Sync.SyncNow(); err != nil {
		t.Fatalf("the same vault with its own key must sync: %v", err)
	}
}

func TestLocalEmptyIsTrueUntilThisMachineHoldsSomething(t *testing.T) {
	h := newHarness(t).unlocked(t)
	if !h.app.Sync.Status().LocalEmpty {
		t.Fatal("a fresh vault is empty")
	}
	host, err := h.app.Host.SaveHost(modelHost("a", 22, ""))
	if err != nil {
		t.Fatal(err)
	}
	if h.app.Sync.Status().LocalEmpty {
		t.Fatal("a vault with a host is not empty")
	}
	if err := h.core.Store.DeleteHost(host.ID); err != nil {
		t.Fatal(err)
	}
	if !h.app.Sync.Status().LocalEmpty {
		t.Fatal("a deleted host still counted")
	}
	if _, err := h.app.Vault.AddPassword("p", "u", "x"); err != nil {
		t.Fatal(err)
	}
	if h.app.Sync.Status().LocalEmpty {
		t.Fatal("a saved password is something")
	}
}
