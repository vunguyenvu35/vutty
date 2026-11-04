package usecase

import (
	"bytes"
	"testing"

	"vutty/internal/domain"
)

// Quick unlock keeps only a local copy of the vault key. Sync is made with the vault's own key and the master
// password, so changing what the keychain holds (a key made unreadable by a new fingerprint, a wrong key, a
// removed item) must neither stop a sync nor send anything different.
func TestSyncIsNotAffectedByWhatTheKeychainHolds(t *testing.T) {
	remote := bareRepo(t)
	a := newHarness(t).unlocked(t)
	host, err := a.app.Host.SaveHost(domain.Host{Label: "kept", Hostname: "k.example"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.app.Vault.EnableBiometric(); err != nil {
		t.Fatal(err)
	}
	if err := a.app.Sync.Configure(remote); err != nil {
		t.Fatal(err)
	}
	if err := a.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}

	// the keychain now holds a key that is not the vault's
	a.app.Vault.Lock()
	if err := a.keychain().Set("vvterm", a.core.DataDir, bytes.Repeat([]byte{0x42}, 32)); err != nil {
		t.Fatal(err)
	}
	if err := a.app.Vault.UnlockBiometric(); err == nil {
		t.Fatal("a wrong key opened the vault")
	}
	if a.app.Vault.Status().KeychainEnabled {
		t.Fatal("the wrong key is still offered")
	}
	if err := a.app.Vault.Unlock(masterPW); err != nil {
		t.Fatal(err)
	}
	h2, err := a.app.Host.SaveHost(domain.Host{Label: "after", Hostname: "after.example"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.app.Sync.SyncNow(); err != nil {
		t.Fatalf("sync after the keychain key was replaced: %v", err)
	}

	// a second machine that joins sees both hosts: what was sent was sealed with the vault's key
	b := newHarness(t)
	if err := b.app.Sync.Configure(remote); err != nil {
		t.Fatal(err)
	}
	if err := b.app.Sync.JoinExisting(masterPW); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{host.ID, h2.ID} {
		if _, err := b.app.Host.GetHost(id); err != nil {
			t.Fatalf("host %s did not reach the second machine: %v", id, err)
		}
	}

	// quick unlock can be turned on again and works
	if err := a.app.Vault.EnableBiometric(); err != nil {
		t.Fatal(err)
	}
	a.app.Vault.Lock()
	if err := a.app.Vault.UnlockBiometric(); err != nil {
		t.Fatalf("quick unlock after turning it on again: %v", err)
	}
	if err := a.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}

	// an empty keychain (the item removed, or lost with a new fingerprint) changes nothing for sync either
	if err := a.app.Vault.DisableBiometric(); err != nil {
		t.Fatal(err)
	}
	if err := a.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}
}
