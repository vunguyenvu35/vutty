package vault

import (
	"bytes"
	"encoding/base64"
	"errors"
	"sync/atomic"

	"github.com/zalando/go-keyring"
)

// plainKeychain stores the DEK in macOS Keychain / Windows Credential Manager / Secret Service as an ordinary
// item: any program that can read the user's keychain can read it.
type plainKeychain struct{}

func (plainKeychain) Set(service, user string, secret []byte) error {
	return keyring.Set(service, user, base64.StdEncoding.EncodeToString(secret))
}

func (plainKeychain) Get(service, user string) ([]byte, error) {
	s, err := keyring.Get(service, user)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(s)
}

func (plainKeychain) Delete(service, user string) error {
	err := keyring.Delete(service, user)
	if err == keyring.ErrNotFound {
		return nil
	}
	return err
}

// ErrSecureUnavailable is what a SecureStore answers when this build or machine cannot make an item that is
// guarded by the fingerprint (for example a build without the keychain entitlement). It is remembered, so the
// store is not tried again.
var ErrSecureUnavailable = errors.New("a keychain item guarded by the fingerprint is not available here")

// SecureStore keeps the key in an item that the operating system itself guards with the fingerprint: reading it
// shows the Touch ID sheet, and a program that is not allowed to ask cannot read it.
type SecureStore interface {
	Set(service, user string, secret []byte) error
	// Get reads the item and shows the system sheet; reason completes "Vutty is trying to …". keyring.ErrNotFound
	// means the item is gone or was made unreadable (the set of fingerprints changed).
	Get(service, user, reason string) ([]byte, error)
	// Has tells whether the item is there, without asking for anything.
	Has(service, user string) bool
	Delete(service, user string) error
}

// getReason is what the system sheet says when the key is read.
const getReason = "unlock your saved keys and passwords"

// TieredKeychain keeps the key in a SecureStore when one can be made, and in a plain item when it cannot. An
// older plain item is moved to the secure store the next time the vault has been opened with the master password
// (Upgrade). Whatever goes wrong with the secure store, the plain item and the master password remain.
type TieredKeychain struct {
	Secure SecureStore
	Plain  Keychain

	off atomic.Bool // the secure store answered ErrSecureUnavailable once
}

func (k *TieredKeychain) secure() SecureStore {
	if k.Secure == nil || k.off.Load() {
		return nil
	}
	return k.Secure
}

// has reports whether the secure item exists.
func (k *TieredKeychain) has(service, user string) bool {
	s := k.secure()
	return s != nil && s.Has(service, user)
}

func (k *TieredKeychain) Set(service, user string, secret []byte) error {
	if s := k.secure(); s != nil {
		err := s.Set(service, user, secret)
		if err == nil {
			_ = k.Plain.Delete(service, user) // a copy that anyone could read must not stay
			return nil
		}
		if errors.Is(err, ErrSecureUnavailable) {
			k.off.Store(true)
		}
		// fall through: a plain item still gives quick unlock behind the app's own Touch ID check
	}
	return k.Plain.Set(service, user, secret)
}

func (k *TieredKeychain) Get(service, user string) ([]byte, error) {
	if k.has(service, user) {
		b, err := k.Secure.Get(service, user, getReason)
		if errors.Is(err, keyring.ErrNotFound) {
			_ = k.Secure.Delete(service, user) // unreadable for good (new fingerprint): the password is asked for
		}
		return b, err
	}
	return k.Plain.Get(service, user)
}

func (k *TieredKeychain) Delete(service, user string) error {
	var err error
	if s := k.secure(); s != nil {
		err = s.Delete(service, user)
	}
	if perr := k.Plain.Delete(service, user); perr != nil {
		return perr
	}
	return err
}

// Has tells whether a key is stored, without reading it (so without a fingerprint prompt).
func (k *TieredKeychain) Has(service, user string) bool {
	if k.has(service, user) {
		return true
	}
	b, err := k.Plain.Get(service, user)
	return err == nil && len(b) > 0
}

// Protected tells whether the stored key is in an item that guards itself: reading it asks for the fingerprint.
func (k *TieredKeychain) Protected(service, user string) bool { return k.has(service, user) }

// Upgrade moves a plain item to the secure store. dek is the key the vault was just opened with; the item is
// moved only when it holds that same key, and left alone when the move fails.
func (k *TieredKeychain) Upgrade(service, user string, dek []byte) {
	s := k.secure()
	if s == nil || s.Has(service, user) {
		return
	}
	plain, err := k.Plain.Get(service, user)
	if err != nil || !bytes.Equal(plain, dek) {
		return
	}
	if err := s.Set(service, user, dek); err != nil {
		if errors.Is(err, ErrSecureUnavailable) {
			k.off.Store(true)
		}
		return
	}
	_ = k.Plain.Delete(service, user)
}

// MemKeychain is an in-memory Keychain for tests and headless runs.
type MemKeychain struct{ m map[string][]byte }

func NewMemKeychain() *MemKeychain { return &MemKeychain{m: map[string][]byte{}} }

func (k *MemKeychain) Set(s, u string, b []byte) error {
	k.m[s+"/"+u] = append([]byte(nil), b...)
	return nil
}

func (k *MemKeychain) Get(s, u string) ([]byte, error) {
	b, ok := k.m[s+"/"+u]
	if !ok {
		return nil, keyring.ErrNotFound
	}
	return append([]byte(nil), b...), nil
}

func (k *MemKeychain) Delete(s, u string) error { delete(k.m, s+"/"+u); return nil }
