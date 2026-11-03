package vault

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vutty/internal/domain"
)

const pw = "correct horse battery"

func fastKDF() KDFParams { return KDFParams{Memory: 64, Time: 1, Threads: 1} }

func newVault(t *testing.T) (*Vault, string, *MemKeychain) {
	t.Helper()
	dir := t.TempDir()
	kc := NewMemKeychain()
	v, err := New(dir, kc)
	if err != nil {
		t.Fatal(err)
	}
	v.SetKDF(fastKDF())
	return v, dir, kc
}

func TestCreateRejectsWeakPassword(t *testing.T) {
	v, _, _ := newVault(t)
	if err := v.Create("short"); err != ErrWeakPassword {
		t.Fatalf("got %v want ErrWeakPassword", err)
	}
	if v.Status().Exists {
		t.Fatal("vault must not exist after rejected create")
	}
}

func TestCreateTwiceFails(t *testing.T) {
	v, _, _ := newVault(t)
	if err := v.Create(pw); err != nil {
		t.Fatal(err)
	}
	if err := v.Create(pw); err != ErrExists {
		t.Fatalf("got %v want ErrExists", err)
	}
}

func TestSecretRoundTripAcrossReopen(t *testing.T) {
	v, dir, _ := newVault(t)
	if err := v.Create(pw); err != nil {
		t.Fatal(err)
	}
	id, err := v.AddSecret(domain.SecretPassword, []byte("hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	v.Lock()
	if _, err := v.GetSecret(id); err != ErrLocked {
		t.Fatalf("locked vault returned %v", err)
	}

	v2, err := New(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := v2.Unlock("wrong password!!"); err != ErrBadPassword {
		t.Fatalf("wrong pw: got %v", err)
	}
	if err := v2.Unlock(pw); err != nil {
		t.Fatal(err)
	}
	got, err := v2.GetSecret(id)
	if err != nil || string(got) != "hunter2" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestCiphertextHasNoPlaintext(t *testing.T) {
	v, dir, _ := newVault(t)
	_ = v.Create(pw)
	_, _ = v.AddSecret(domain.SecretPrivateKey, []byte("TOP-SECRET-MARKER"))
	raw, _ := os.ReadFile(filepath.Join(dir, "vault.json"))
	if bytes.Contains(raw, []byte("TOP-SECRET-MARKER")) {
		t.Fatal("plaintext leaked into vault.json")
	}
	st, _ := os.Stat(filepath.Join(dir, "vault.json"))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("vault.json perm %v, want 0600", st.Mode().Perm())
	}
}

func TestSwappedCiphertextIsRejected(t *testing.T) {
	v, _, _ := newVault(t)
	_ = v.Create(pw)
	a, _ := v.AddSecret(domain.SecretPassword, []byte("aaa"))
	b, _ := v.AddSecret(domain.SecretPassword, []byte("bbb"))
	s := v.Secrets()
	// swap ciphertext+nonce between records; AAD (secret id) must catch it
	s[0].Ciphertext, s[1].Ciphertext = s[1].Ciphertext, s[0].Ciphertext
	s[0].Nonce, s[1].Nonce = s[1].Nonce, s[0].Nonce
	if err := v.ReplaceSecrets(s); err != nil {
		t.Fatal(err)
	}
	if _, err := v.GetSecret(a); err == nil {
		t.Fatal("swapped secret a decrypted")
	}
	if _, err := v.GetSecret(b); err == nil {
		t.Fatal("swapped secret b decrypted")
	}
}

func TestChangePasswordKeepsSecrets(t *testing.T) {
	v, dir, _ := newVault(t)
	_ = v.Create(pw)
	id, _ := v.AddSecret(domain.SecretPassword, []byte("keep-me"))
	if err := v.ChangePassword("not the password", "another long password"); err != ErrBadPassword {
		t.Fatalf("got %v", err)
	}
	if err := v.ChangePassword(pw, "tiny"); err != ErrWeakPassword {
		t.Fatalf("got %v", err)
	}
	if err := v.ChangePassword(pw, "another long password"); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	v2, _ := New(dir, nil)
	if err := v2.Unlock(pw); err != ErrBadPassword {
		t.Fatalf("old password still works: %v", err)
	}
	if err := v2.Unlock("another long password"); err != nil {
		t.Fatal(err)
	}
	if got, _ := v2.GetSecret(id); string(got) != "keep-me" {
		t.Fatalf("secret lost after password change: %q", got)
	}
}

func TestDeleteSecretWipesCiphertext(t *testing.T) {
	v, _, _ := newVault(t)
	_ = v.Create(pw)
	id, _ := v.AddSecret(domain.SecretPassword, []byte("x"))
	if err := v.DeleteSecret(id); err != nil {
		t.Fatal(err)
	}
	if _, err := v.GetSecret(id); err != ErrSecretMissing {
		t.Fatalf("got %v", err)
	}
	s := v.Secrets()
	if len(s) != 1 || s[0].DeletedAt == nil || len(s[0].Ciphertext) != 0 {
		t.Fatalf("tombstone not kept correctly: %+v", s)
	}
}

func TestKeychainQuickUnlock(t *testing.T) {
	v, dir, kc := newVault(t)
	_ = v.Create(pw)
	id, _ := v.AddSecret(domain.SecretPassword, []byte("zzz"))
	if err := v.EnableKeychain(); err != nil {
		t.Fatal(err)
	}
	if !v.Status().KeychainEnabled {
		t.Fatal("keychain should be enabled")
	}
	v.Lock()
	v2, _ := New(dir, kc)
	if err := v2.UnlockKeychain(); err != nil {
		t.Fatal(err)
	}
	if got, _ := v2.GetSecret(id); string(got) != "zzz" {
		t.Fatalf("got %q", got)
	}
	if err := v2.DisableKeychain(); err != nil {
		t.Fatal(err)
	}
	v2.Lock()
	if err := v2.UnlockKeychain(); err != ErrNoKeychain {
		t.Fatalf("got %v", err)
	}
}

func TestKeychainWithForeignDEKIsRejectedAndPurged(t *testing.T) {
	v, dir, kc := newVault(t)
	_ = v.Create(pw)
	_, _ = v.AddSecret(domain.SecretPassword, []byte("zzz"))
	v.Lock()
	_ = kc.Set("vvterm", dir, bytes.Repeat([]byte{7}, 32))
	if err := v.UnlockKeychain(); err != ErrNoKeychain {
		t.Fatalf("got %v", err)
	}
	if _, err := kc.Get("vvterm", dir); err == nil {
		t.Fatal("bad DEK should have been purged from keychain")
	}
}

func TestEnableKeychainRequiresUnlock(t *testing.T) {
	v, _, _ := newVault(t)
	_ = v.Create(pw)
	v.Lock()
	if err := v.EnableKeychain(); err != ErrLocked {
		t.Fatalf("got %v", err)
	}
}

func TestAutoLockFiresCallback(t *testing.T) {
	v, _, _ := newVault(t)
	_ = v.Create(pw)
	fired := make(chan struct{}, 1)
	v.OnLock(func() { fired <- struct{}{} })
	v.SetAutoLock(50 * time.Millisecond)
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("auto-lock did not fire")
	}
	if v.Status().Unlocked {
		t.Fatal("vault still unlocked after auto-lock")
	}
}

func TestActivityPostponesAutoLock(t *testing.T) {
	v, _, _ := newVault(t)
	_ = v.Create(pw)
	id, _ := v.AddSecret(domain.SecretPassword, []byte("a"))
	v.SetAutoLock(300 * time.Millisecond)
	for i := 0; i < 4; i++ {
		time.Sleep(120 * time.Millisecond)
		if _, err := v.GetSecret(id); err != nil {
			t.Fatalf("locked despite activity at iter %d: %v", i, err)
		}
	}
}

func TestTouchPostponesAutoLockButNeverUnlocks(t *testing.T) {
	v, _, _ := newVault(t)
	v.Touch() // locked: must be a no-op, not a panic or an unlock
	if v.Status().Unlocked {
		t.Fatal("Touch unlocked the vault")
	}
	_ = v.Create(pw)
	v.SetAutoLock(300 * time.Millisecond)
	for i := 0; i < 6; i++ {
		time.Sleep(120 * time.Millisecond)
		v.Touch()
		if !v.Status().Unlocked {
			t.Fatalf("locked despite terminal activity at iter %d", i)
		}
	}
	time.Sleep(600 * time.Millisecond)
	if v.Status().Unlocked {
		t.Fatal("vault stayed unlocked without activity")
	}
}

func TestManualLockFiresCallbackOnce(t *testing.T) {
	v, _, _ := newVault(t)
	_ = v.Create(pw)
	n := 0
	v.OnLock(func() { n++ })
	v.Lock()
	v.Lock()
	if n != 1 {
		t.Fatalf("callback fired %d times", n)
	}
}

func TestBlobSealOpenAndAAD(t *testing.T) {
	v, _, _ := newVault(t)
	_ = v.Create(pw)
	b, err := v.SealBlob([]byte("bundle"), []byte("aad1"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := v.OpenBlob(b, []byte("aad1")); err != nil || string(got) != "bundle" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := v.OpenBlob(b, []byte("other")); err == nil {
		t.Fatal("wrong AAD accepted")
	}
	b[len(b)-1] ^= 1
	if _, err := v.OpenBlob(b, []byte("aad1")); err == nil {
		t.Fatal("tampered blob accepted")
	}
}

func TestAdoptOnNewMachine(t *testing.T) {
	v, _, _ := newVault(t)
	_ = v.Create(pw)
	id, _ := v.AddSecret(domain.SecretPassword, []byte("shared"))
	h, _ := v.Header()
	secrets := v.Secrets()

	v2, _ := New(t.TempDir(), nil)
	if err := v2.Adopt("bad password here", h, secrets); err != ErrBadPassword {
		t.Fatalf("got %v", err)
	}
	if err := v2.Adopt(pw, h, secrets); err != nil {
		t.Fatal(err)
	}
	if got, _ := v2.GetSecret(id); string(got) != "shared" {
		t.Fatalf("got %q", got)
	}
}

func TestMaliciousKDFParamsRejected(t *testing.T) {
	v, _, _ := newVault(t)
	_ = v.Create(pw)
	h, _ := v.Header()
	h.KDF.Memory = 1 << 30 // 1 TiB
	v2, _ := New(t.TempDir(), nil)
	if err := v2.Adopt(pw, h, nil); err == nil || err == ErrBadPassword {
		t.Fatalf("expected KDF validation error, got %v", err)
	}
	h.KDF.Threads = 0
	h.KDF.Memory = 64
	if err := v2.Adopt(pw, h, nil); err == nil {
		t.Fatal("zero threads accepted")
	}
}

func TestPasswordBackupRoundTrip(t *testing.T) {
	blob, err := SealWithPassword("backup-pass", []byte(`{"a":1}`), fastKDF())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), `"a":1`) {
		t.Fatal("plaintext in backup")
	}
	if got, err := OpenWithPassword("backup-pass", blob); err != nil || string(got) != `{"a":1}` {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := OpenWithPassword("nope", blob); err != ErrBadPassword {
		t.Fatalf("got %v", err)
	}
	if _, err := OpenWithPassword("x", []byte("garbage")); err == nil {
		t.Fatal("garbage accepted")
	}
	var m map[string]any
	_ = json.Unmarshal(blob, &m)
	m["kdf"] = map[string]any{"salt": "AAAAAAAAAAAAAAAAAAAAAA==", "m_kib": 4000000000, "t": 1, "p": 1}
	bad, _ := json.Marshal(m)
	if _, err := OpenWithPassword("x", bad); err == nil || err == ErrBadPassword {
		t.Fatalf("hostile KDF accepted: %v", err)
	}
}

func TestGenerateAndParseKey(t *testing.T) {
	priv, pub, err := GenerateEd25519("vutty-test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub, "ssh-ed25519 ") {
		t.Fatalf("pub = %q", pub)
	}
	s, err := ParsePrivateKey(priv, nil)
	if err != nil {
		t.Fatal(err)
	}
	if PublicLine(s) != strings.Fields(pub)[0]+" "+strings.Fields(pub)[1] {
		t.Fatalf("public mismatch: %q vs %q", PublicLine(s), pub)
	}
	if _, err := ParsePrivateKey([]byte("not a key"), nil); err == nil {
		t.Fatal("garbage parsed")
	}
}

func TestDestroyWorksWhileLockedAndClearsKeychain(t *testing.T) {
	v, dir, kc := newVault(t)
	_ = v.Create(pw)
	_, _ = v.AddSecret(domain.SecretPassword, []byte("x"))
	_ = v.EnableKeychain()
	v.Lock()
	if err := v.Destroy(); err != nil {
		t.Fatal(err)
	}
	if v.Status().Exists || v.Status().KeychainEnabled {
		t.Fatalf("%+v", v.Status())
	}
	if _, err := kc.Get("vvterm", dir); err == nil {
		t.Fatal("DEK left in the keychain")
	}
	if _, err := os.Stat(filepath.Join(dir, "vault.json")); !os.IsNotExist(err) {
		t.Fatalf("vault.json still exists: %v", err)
	}
	v2, _ := New(dir, nil)
	if v2.Status().Exists {
		t.Fatal("vault came back after reopen")
	}
	if err := v.Create("another long password"); err != nil {
		t.Fatalf("cannot create after destroy: %v", err)
	}
	if err := v.Destroy(); err != nil {
		t.Fatal(err)
	}
	if err := v.Destroy(); err != nil {
		t.Fatalf("destroy must be idempotent: %v", err)
	}
}

func TestHintSurvivesLockReopenAndPasswordChange(t *testing.T) {
	v, dir, _ := newVault(t)
	if err := v.Create(pw); err != nil {
		t.Fatal(err)
	}
	if err := v.SetHint("first pet + street"); err != nil {
		t.Fatal(err)
	}
	if err := v.ChangePassword(pw, "another long password!"); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	if got := v.Status().Hint; got != "first pet + street" {
		t.Fatalf("hint while locked = %q", got)
	}
	v2, err := New(dir, NewMemKeychain())
	if err != nil {
		t.Fatal(err)
	}
	if got := v2.Status().Hint; got != "first pet + street" {
		t.Fatalf("hint after reopen = %q", got)
	}
	if err := v2.SetHint("x"); err != ErrLocked {
		t.Fatalf("a locked vault must not change the hint, got %v", err)
	}
}

func TestHintTooLongIsRejected(t *testing.T) {
	v, _, _ := newVault(t)
	if err := v.Create(pw); err != nil {
		t.Fatal(err)
	}
	if err := v.SetHint(strings.Repeat("a", MaxHintLen+1)); err == nil {
		t.Fatal("expected an error")
	}
}

func TestKeychainNeedsTheAuthenticator(t *testing.T) {
	v, _, _ := newVault(t)
	if err := v.Create(pw); err != nil {
		t.Fatal(err)
	}
	asked := 0
	deny := false
	v.SetAuthenticator(func(string) error {
		asked++
		if deny {
			return errCancelled
		}
		return nil
	})
	if err := v.EnableKeychain(); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	deny = true
	if err := v.UnlockKeychain(); err != errCancelled || v.Status().Unlocked {
		t.Fatalf("a refused Touch ID must keep the vault locked, got %v", err)
	}
	deny = false
	if err := v.UnlockKeychain(); err != nil || !v.Status().Unlocked {
		t.Fatal(err)
	}
	if asked != 3 {
		t.Fatalf("asked %d times, want 3", asked)
	}
}

var errCancelled = os.ErrPermission

// ---- the key check: a key that comes from the keychain must be the vault's own ----

func TestKeychainRefusesAStaleKeyEvenWhenTheVaultHasNoSecrets(t *testing.T) {
	v, dir, kc := newVault(t)
	if err := v.Create(pw); err != nil {
		t.Fatal(err)
	}
	if len(v.f.Header.KeyCheck) == 0 {
		t.Fatal("a new vault must carry a key check")
	}
	// the keychain holds a key left by an older vault of the same place
	stale := bytes.Repeat([]byte{7}, dekLen)
	if err := kc.Set("vvterm", dir, stale); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	if err := v.UnlockKeychain(); err != ErrNoKeychain {
		t.Fatalf("a stale key was accepted: %v", err)
	}
	if st := v.Status(); st.Unlocked {
		t.Fatal("unlocked with a key of another vault")
	}
	if _, err := kc.Get("vvterm", dir); err == nil {
		t.Fatal("the stale key must be removed so it is not offered again")
	}
	// the master password still opens it
	if err := v.Unlock(pw); err != nil {
		t.Fatal(err)
	}
}

func TestKeychainUnlockStillWorksWithTheRightKey(t *testing.T) {
	v, _, _ := newVault(t)
	if err := v.Create(pw); err != nil {
		t.Fatal(err)
	}
	if err := v.EnableKeychain(); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	if err := v.UnlockKeychain(); err != nil {
		t.Fatalf("the vault's own key must open it: %v", err)
	}
}

func TestAVaultFromBeforeTheKeyCheckGetsOneAtTheNextPasswordUnlock(t *testing.T) {
	v, dir, kc := newVault(t)
	if err := v.Create(pw); err != nil {
		t.Fatal(err)
	}
	if err := v.EnableKeychain(); err != nil {
		t.Fatal(err)
	}
	v.f.Header.KeyCheck = nil // what an older vault looks like
	if err := v.flush(); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	// with no check and no secret a key from outside cannot be trusted: the password is asked for, the entry stays
	if err := v.UnlockKeychain(); err != ErrNoKeychain {
		t.Fatalf("an unverifiable key was accepted: %v", err)
	}
	if _, err := kc.Get("vvterm", dir); err != nil {
		t.Fatal("an entry that could not be checked must not be deleted")
	}
	if err := v.Unlock(pw); err != nil {
		t.Fatal(err)
	}
	if len(v.f.Header.KeyCheck) == 0 {
		t.Fatal("the password unlock must add the check")
	}
	v.Lock()
	if err := v.UnlockKeychain(); err != nil {
		t.Fatalf("with the check in place the keychain works again: %v", err)
	}
}

func TestKeyCheckSurvivesChangePasswordAndAdopt(t *testing.T) {
	v, _, _ := newVault(t)
	if err := v.Create(pw); err != nil {
		t.Fatal(err)
	}
	if err := v.ChangePassword(pw, "a brand new master password"); err != nil {
		t.Fatal(err)
	}
	h, _ := v.Header()
	if len(h.KeyCheck) == 0 {
		t.Fatal("ChangePassword lost the key check")
	}
	v.Lock()
	if err := v.UnlockKeychain(); err == nil {
		t.Fatal("no keychain entry: it must not unlock")
	}
	// a header from a repository made before the check existed gets one when it is adopted
	old := h
	old.KeyCheck = nil
	v2, _, _ := newVault(t)
	if err := v2.Adopt("a brand new master password", old, nil); err != nil {
		t.Fatal(err)
	}
	h2, _ := v2.Header()
	if len(h2.KeyCheck) == 0 {
		t.Fatal("Adopt must add the check")
	}
}

func TestCheckKeyAndUnreadable(t *testing.T) {
	v, _, _ := newVault(t)
	if err := v.CheckKey(); err != ErrNoVault {
		t.Fatalf("no vault: %v", err)
	}
	if err := v.Create(pw); err != nil {
		t.Fatal(err)
	}
	good, _ := v.AddSecret(domain.SecretPassword, []byte("one"))
	if err := v.CheckKey(); err != nil {
		t.Fatalf("the key of the vault was refused: %v", err)
	}
	if bad, err := v.Unreadable(); err != nil || len(bad) != 0 {
		t.Fatalf("%v %v", bad, err)
	}

	// a secret that another vault sealed arrives with a sync: it is listed, nothing else is
	other, _, _ := newVault(t)
	if err := other.Create("another master password"); err != nil {
		t.Fatal(err)
	}
	foreign, _ := other.AddSecret(domain.SecretPassword, []byte("two"))
	if err := v.ReplaceSecrets(append(v.Secrets(), other.Secrets()...)); err != nil {
		t.Fatal(err)
	}
	bad, err := v.Unreadable()
	if err != nil || len(bad) != 1 || bad[0] != foreign {
		t.Fatalf("unreadable %v %v (good one was %s)", bad, err, good)
	}
	// a deleted secret is not reported
	if err := v.DeleteSecret(foreign); err != nil {
		t.Fatal(err)
	}
	if bad, _ := v.Unreadable(); len(bad) != 0 {
		t.Fatalf("a deleted secret was reported: %v", bad)
	}

	// a key that is not the header's (a leftover from another vault) is caught
	v.mu.Lock()
	realKey := v.dek
	v.dek = bytes.Repeat([]byte{7}, len(realKey))
	v.mu.Unlock()
	if err := v.CheckKey(); err != ErrKeyMismatch {
		t.Fatalf("a wrong key passed: %v", err)
	}
	v.Lock()
	if err := v.CheckKey(); err != ErrLocked {
		t.Fatalf("locked: %v", err)
	}
	if _, err := v.Unreadable(); err != ErrLocked {
		t.Fatalf("locked: %v", err)
	}
}
