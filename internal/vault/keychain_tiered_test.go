package vault

import (
	"bytes"
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

// fakeSecure is a SecureStore in memory. fail makes Set answer with that error; gone makes Get answer "not found"
// (the set of fingerprints changed) while the item is still listed.
type fakeSecure struct {
	m     map[string][]byte
	fail  error
	gone  bool
	reads int
}

func newFakeSecure() *fakeSecure { return &fakeSecure{m: map[string][]byte{}} }

func (f *fakeSecure) Set(s, u string, b []byte) error {
	if f.fail != nil {
		return f.fail
	}
	f.m[s+"/"+u] = append([]byte(nil), b...)
	return nil
}

func (f *fakeSecure) Get(s, u, _ string) ([]byte, error) {
	f.reads++
	if f.gone {
		return nil, keyring.ErrNotFound
	}
	b, ok := f.m[s+"/"+u]
	if !ok {
		return nil, keyring.ErrNotFound
	}
	return append([]byte(nil), b...), nil
}

func (f *fakeSecure) Has(s, u string) bool { _, ok := f.m[s+"/"+u]; return ok }
func (f *fakeSecure) Delete(s, u string) error {
	delete(f.m, s+"/"+u)
	return nil
}

func TestTieredKeepsTheKeyInTheGuardedItemAndNowhereElse(t *testing.T) {
	sec, plain := newFakeSecure(), NewMemKeychain()
	k := &TieredKeychain{Secure: sec, Plain: plain}
	plain.Set("vvterm", "a", []byte("old plain copy"))
	if err := k.Set("vvterm", "a", bytes.Repeat([]byte{7}, 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Get("vvterm", "a"); err == nil {
		t.Fatal("a plain copy stayed next to the guarded item")
	}
	if !k.Protected("vvterm", "a") || !k.Has("vvterm", "a") {
		t.Fatal("the guarded item is not reported")
	}
	if b, err := k.Get("vvterm", "a"); err != nil || !bytes.Equal(b, bytes.Repeat([]byte{7}, 32)) {
		t.Fatalf("%v %x", err, b)
	}
	if err := k.Delete("vvterm", "a"); err != nil || k.Has("vvterm", "a") {
		t.Fatalf("delete: %v has=%v", err, k.Has("vvterm", "a"))
	}
}

// A build that may not make a guarded item (an ad hoc signed app) still gets quick unlock, from a plain item.
func TestTieredFallsBackToAPlainItemWhenTheGuardedOneIsNotAllowed(t *testing.T) {
	sec, plain := newFakeSecure(), NewMemKeychain()
	sec.fail = ErrSecureUnavailable
	k := &TieredKeychain{Secure: sec, Plain: plain}
	key := bytes.Repeat([]byte{3}, 32)
	if err := k.Set("vvterm", "a", key); err != nil {
		t.Fatalf("the fallback must not fail: %v", err)
	}
	if k.Protected("vvterm", "a") {
		t.Fatal("reported as guarded")
	}
	if b, err := k.Get("vvterm", "a"); err != nil || !bytes.Equal(b, key) {
		t.Fatalf("%v", err)
	}
	// the secure store is not asked again once it said it is not available
	sec.fail = nil
	k.Set("vvterm", "a", key)
	if sec.Has("vvterm", "a") {
		t.Fatal("the secure store was tried again")
	}
}

func TestTieredFallsBackWhenTheGuardedItemFailsForAnotherReason(t *testing.T) {
	sec, plain := newFakeSecure(), NewMemKeychain()
	sec.fail = errors.New("boom")
	k := &TieredKeychain{Secure: sec, Plain: plain}
	if err := k.Set("vvterm", "a", bytes.Repeat([]byte{1}, 32)); err != nil {
		t.Fatal(err)
	}
	if !k.Has("vvterm", "a") || k.Protected("vvterm", "a") {
		t.Fatal("the plain item should hold the key")
	}
	sec.fail = nil // a later attempt may work
	k.Set("vvterm", "a", bytes.Repeat([]byte{1}, 32))
	if !k.Protected("vvterm", "a") {
		t.Fatal("a transient failure must not turn the guarded item off for good")
	}
}

func TestTieredUpgradeMovesAPlainItemOnlyWhenItIsTheSameKey(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	sec, plain := newFakeSecure(), NewMemKeychain()
	k := &TieredKeychain{Secure: sec, Plain: plain}
	plain.Set("vvterm", "a", key)

	k.Upgrade("vvterm", "a", bytes.Repeat([]byte{8}, 32)) // another key: leave it
	if k.Protected("vvterm", "a") {
		t.Fatal("a plain item with another key was moved")
	}
	k.Upgrade("vvterm", "a", key)
	if !k.Protected("vvterm", "a") {
		t.Fatal("not moved")
	}
	if _, err := plain.Get("vvterm", "a"); err == nil {
		t.Fatal("the plain copy was not removed")
	}
	if sec.reads != 0 {
		t.Fatal("moving must not need the fingerprint")
	}

	// a move that fails leaves the plain item as it was
	sec2, plain2 := newFakeSecure(), NewMemKeychain()
	sec2.fail = ErrSecureUnavailable
	k2 := &TieredKeychain{Secure: sec2, Plain: plain2}
	plain2.Set("vvterm", "a", key)
	k2.Upgrade("vvterm", "a", key)
	if b, err := plain2.Get("vvterm", "a"); err != nil || !bytes.Equal(b, key) {
		t.Fatal("the plain item was lost by a failed move")
	}
}

// After the fingerprints change the guarded item cannot be read; it is removed, so the master password is asked.
func TestTieredRemovesAnItemMadeUnreadable(t *testing.T) {
	sec, plain := newFakeSecure(), NewMemKeychain()
	k := &TieredKeychain{Secure: sec, Plain: plain}
	k.Set("vvterm", "a", bytes.Repeat([]byte{5}, 32))
	sec.gone = true
	if _, err := k.Get("vvterm", "a"); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if k.Has("vvterm", "a") {
		t.Fatal("the unreadable item is still offered")
	}
}

// --- the vault on top of it ---

// probing is a Keychain that records reads, like a real guarded one that prompts on every read.
type probing struct {
	*TieredKeychain
	sec *fakeSecure
}

func newProbing() *probing {
	sec := newFakeSecure()
	return &probing{TieredKeychain: &TieredKeychain{Secure: sec, Plain: NewMemKeychain()}, sec: sec}
}

func TestStatusDoesNotReadAGuardedKey(t *testing.T) {
	dir := t.TempDir()
	kc := newProbing()
	v, err := New(dir, kc)
	if err != nil {
		t.Fatal(err)
	}
	v.SetKDF(fastKDF())
	if err := v.Create(pw); err != nil {
		t.Fatal(err)
	}
	if err := v.EnableKeychain(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if !v.Status().KeychainEnabled {
			t.Fatal("not enabled")
		}
	}
	if kc.sec.reads != 0 {
		t.Fatalf("Status read the guarded key %d times (each would show Touch ID)", kc.sec.reads)
	}
}

func TestAGuardedItemIsNotAskedForTouchIDTwice(t *testing.T) {
	dir := t.TempDir()
	kc := newProbing()
	v, _ := New(dir, kc)
	v.SetKDF(fastKDF())
	v.Create(pw)
	asked := 0
	v.SetAuthenticator(func(string) error { asked++; return nil })
	if err := v.EnableKeychain(); err != nil {
		t.Fatal(err)
	}
	if asked != 1 {
		t.Fatalf("enabling asked %d times", asked)
	}
	v.Lock()
	if err := v.UnlockKeychain(); err != nil {
		t.Fatal(err)
	}
	if asked != 1 || kc.sec.reads != 1 {
		t.Fatalf("the app asked %d times and the item was read %d times: the item's own sheet is the check", asked, kc.sec.reads)
	}
}

// The old plain item moves into the guarded one the next time the vault is opened, with the password or with
// the plain item itself.
func TestOldPlainItemIsUpgradedOnTheNextUnlock(t *testing.T) {
	for _, how := range []string{"password", "keychain"} {
		t.Run(how, func(t *testing.T) {
			dir := t.TempDir()
			plain := NewMemKeychain()
			old := &TieredKeychain{Plain: plain} // a build that could only make plain items
			v, _ := New(dir, old)
			v.SetKDF(fastKDF())
			v.Create(pw)
			if err := v.EnableKeychain(); err != nil {
				t.Fatal(err)
			}
			v.Lock()

			sec := newFakeSecure()
			kc := &TieredKeychain{Secure: sec, Plain: plain}
			v2, err := New(dir, kc)
			if err != nil {
				t.Fatal(err)
			}
			if kc.Protected("vvterm", dir) {
				t.Fatal("guarded before the upgrade")
			}
			if how == "password" {
				err = v2.Unlock(pw)
			} else {
				err = v2.UnlockKeychain()
			}
			if err != nil {
				t.Fatal(err)
			}
			if !kc.Protected("vvterm", dir) {
				t.Fatal("the item was not upgraded")
			}
			if _, err := plain.Get("vvterm", dir); err == nil {
				t.Fatal("the plain copy stayed")
			}
			// and the upgraded item still unlocks the vault
			v2.Lock()
			if err := v2.UnlockKeychain(); err != nil {
				t.Fatalf("quick unlock after the upgrade: %v", err)
			}
		})
	}
}

// Nothing here may block the master password: a store that fails every time still leaves the vault usable.
func TestAFailingGuardedStoreNeverBlocksUnlock(t *testing.T) {
	dir := t.TempDir()
	sec := newFakeSecure()
	sec.fail = ErrSecureUnavailable
	kc := &TieredKeychain{Secure: sec, Plain: NewMemKeychain()}
	v, _ := New(dir, kc)
	v.SetKDF(fastKDF())
	v.Create(pw)
	if err := v.EnableKeychain(); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	if err := v.UnlockKeychain(); err != nil {
		t.Fatalf("the plain item should unlock: %v", err)
	}
	v.Lock()
	if err := v.Unlock(pw); err != nil {
		t.Fatal(err)
	}
}

// A key that biometrics made unreadable is dropped, and the master password gets in.
func TestUnreadableGuardedKeyFallsBackToThePassword(t *testing.T) {
	dir := t.TempDir()
	kc := newProbing()
	v, _ := New(dir, kc)
	v.SetKDF(fastKDF())
	v.Create(pw)
	v.EnableKeychain()
	v.Lock()
	kc.sec.gone = true
	if err := v.UnlockKeychain(); err != ErrNoKeychain {
		t.Fatalf("got %v", err)
	}
	if v.Status().KeychainEnabled {
		t.Fatal("the dead item is still offered")
	}
	if err := v.Unlock(pw); err != nil {
		t.Fatal(err)
	}
}
