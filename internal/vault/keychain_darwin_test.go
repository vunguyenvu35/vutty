//go:build darwin

package vault

import (
	"bytes"
	"errors"
	"testing"
)

// The real macOS store, as far as a test binary can reach it: without the keychain-access-groups entitlement
// (a test binary, or an ad hoc signed app) it must say "not available" and never leave anything behind. When the
// binary does have the entitlement the test checks the round trip instead, which needs the fingerprint sheet
// to be answered, so it is skipped.
func TestRealGuardedStoreSaysNotAvailableWithoutTheEntitlement(t *testing.T) {
	var s cgoSecure
	const svc, user = "vvterm-test", "guarded-store-test"
	t.Cleanup(func() { _ = s.Delete(svc, user) })
	err := s.Set(svc, user, bytes.Repeat([]byte{1}, 32))
	if err == nil {
		t.Skip("this binary may make guarded items; reading one needs the fingerprint, which a test cannot give")
	}
	if !errors.Is(err, ErrSecureUnavailable) {
		t.Fatalf("want ErrSecureUnavailable (an OSStatus 34018 without the entitlement), got %v", err)
	}
	if s.Has(svc, user) {
		t.Fatal("an item exists after a failed Set")
	}
	if err := s.Delete(svc, user); err != nil {
		t.Fatalf("deleting what is not there must not fail: %v", err)
	}
}
