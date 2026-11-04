//go:build darwin

package vault

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Security -framework LocalAuthentication -framework Foundation
#import <Foundation/Foundation.h>
#import <Security/Security.h>
#import <LocalAuthentication/LocalAuthentication.h>
#include <stdlib.h>
#include <string.h>

// Items live in the data protection keychain: only it supports an access control that asks for the
// fingerprint (the file based login keychain, which go-keyring uses, does not).
static NSMutableDictionary *bioQuery(const char *service, const char *account) {
    NSMutableDictionary *q = [NSMutableDictionary dictionary];
    q[(__bridge id)kSecClass] = (__bridge id)kSecClassGenericPassword;
    q[(__bridge id)kSecAttrService] = [NSString stringWithUTF8String:service];
    q[(__bridge id)kSecAttrAccount] = [NSString stringWithUTF8String:account];
    q[(__bridge id)kSecUseDataProtectionKeychain] = @YES;
    return q;
}

// bioSet replaces the item. Returns 0 or an OSStatus (never 0 for a failure).
static int bioSet(const char *service, const char *account, const void *data, int len) {
    CFErrorRef cerr = NULL;
    SecAccessControlRef ac = SecAccessControlCreateWithFlags(kCFAllocatorDefault,
        kSecAttrAccessibleWhenUnlockedThisDeviceOnly, kSecAccessControlBiometryCurrentSet, &cerr);
    if (ac == NULL) {
        int code = cerr ? (int)CFErrorGetCode(cerr) : -1;
        if (cerr) CFRelease(cerr);
        return code ? code : -1;
    }
    SecItemDelete((__bridge CFDictionaryRef)bioQuery(service, account));
    NSMutableDictionary *q = bioQuery(service, account);
    q[(__bridge id)kSecValueData] = [NSData dataWithBytes:data length:len];
    q[(__bridge id)kSecAttrAccessControl] = (__bridge id)ac;
    OSStatus st = SecItemAdd((__bridge CFDictionaryRef)q, NULL);
    CFRelease(ac);
    return (int)st;
}

// bioGet reads the item; the system asks for the fingerprint (reason is shown in the sheet).
static int bioGet(const char *service, const char *account, const char *reason, void *out, int cap, int *n) {
    NSMutableDictionary *q = bioQuery(service, account);
    q[(__bridge id)kSecReturnData] = @YES;
    q[(__bridge id)kSecMatchLimit] = (__bridge id)kSecMatchLimitOne;
    LAContext *ctx = [[LAContext alloc] init];
    ctx.localizedReason = [NSString stringWithUTF8String:reason];
    q[(__bridge id)kSecUseAuthenticationContext] = ctx;
    CFTypeRef res = NULL;
    OSStatus st = SecItemCopyMatching((__bridge CFDictionaryRef)q, &res);
    if (st != errSecSuccess) return (int)st;
    NSData *d = (__bridge NSData *)res;
    int rc = 0;
    if ((int)d.length > cap) rc = -2;
    else {
        memcpy(out, d.bytes, d.length);
        *n = (int)d.length;
    }
    CFRelease(res);
    return rc;
}

// bioExists tells whether the item is there without asking for anything (attributes need no authentication).
static int bioExists(const char *service, const char *account) {
    NSMutableDictionary *q = bioQuery(service, account);
    q[(__bridge id)kSecReturnAttributes] = @YES;
    q[(__bridge id)kSecMatchLimit] = (__bridge id)kSecMatchLimitOne;
    CFTypeRef res = NULL;
    OSStatus st = SecItemCopyMatching((__bridge CFDictionaryRef)q, &res);
    if (res) CFRelease(res);
    return (int)st;
}

static int bioDelete(const char *service, const char *account) {
    return (int)SecItemDelete((__bridge CFDictionaryRef)bioQuery(service, account));
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/zalando/go-keyring"
)

// OSStatus codes this file reacts to.
const (
	errSecSuccess         = 0
	errSecItemNotFound    = -25300
	errSecInteractionNot  = -25308
	errSecMissingEntitle  = -34018 // the app has no keychain-access-groups entitlement (an unsigned or ad hoc build)
	errSecNoSuchAttribute = -25303
)

type osStatus int

func (s osStatus) Error() string { return fmt.Sprintf("keychain status %d", int(s)) }

// statusErr turns an OSStatus into the error the vault understands.
func statusErr(st C.int) error {
	switch int(st) {
	case errSecSuccess:
		return nil
	case errSecItemNotFound:
		return keyring.ErrNotFound
	case errSecMissingEntitle, errSecNoSuchAttribute:
		return fmt.Errorf("%w: %w", ErrSecureUnavailable, osStatus(st))
	}
	return osStatus(st)
}

// cgoSecure is the data protection keychain with an access control that asks for the fingerprint
// (kSecAccessControlBiometryCurrentSet): a new fingerprint set makes the item unreadable.
type cgoSecure struct{}

func (cgoSecure) Set(service, user string, secret []byte) error {
	cs, cu := C.CString(service), C.CString(user)
	defer C.free(unsafe.Pointer(cs))
	defer C.free(unsafe.Pointer(cu))
	if len(secret) == 0 {
		return errors.New("empty key")
	}
	return statusErr(C.bioSet(cs, cu, unsafe.Pointer(&secret[0]), C.int(len(secret))))
}

func (cgoSecure) Get(service, user, reason string) ([]byte, error) {
	cs, cu, cr := C.CString(service), C.CString(user), C.CString(reason)
	defer C.free(unsafe.Pointer(cs))
	defer C.free(unsafe.Pointer(cu))
	defer C.free(unsafe.Pointer(cr))
	buf := make([]byte, 256)
	var n C.int
	if err := statusErr(C.bioGet(cs, cu, cr, unsafe.Pointer(&buf[0]), C.int(len(buf)), &n)); err != nil {
		return nil, err
	}
	return buf[:int(n)], nil
}

func (cgoSecure) Has(service, user string) bool {
	cs, cu := C.CString(service), C.CString(user)
	defer C.free(unsafe.Pointer(cs))
	defer C.free(unsafe.Pointer(cu))
	st := int(C.bioExists(cs, cu))
	return st == errSecSuccess || st == errSecInteractionNot
}

func (cgoSecure) Delete(service, user string) error {
	cs, cu := C.CString(service), C.CString(user)
	defer C.free(unsafe.Pointer(cs))
	defer C.free(unsafe.Pointer(cu))
	// without the entitlement nothing of ours can be stored there, so there is nothing to delete
	if err := statusErr(C.bioDelete(cs, cu)); err != nil && !errors.Is(err, keyring.ErrNotFound) && !errors.Is(err, ErrSecureUnavailable) {
		return err
	}
	return nil
}

var osTiered = &TieredKeychain{Secure: cgoSecure{}, Plain: plainKeychain{}}

// OSKeychain keeps the DEK in an item that macOS guards with Touch ID when the build is allowed to make one (it
// needs the keychain-access-groups entitlement, which an ad hoc signed build does not have), and in an ordinary
// login keychain item otherwise.
type OSKeychain struct{}

func (OSKeychain) Set(service, user string, secret []byte) error {
	return osTiered.Set(service, user, secret)
}
func (OSKeychain) Get(service, user string) ([]byte, error) { return osTiered.Get(service, user) }
func (OSKeychain) Delete(service, user string) error        { return osTiered.Delete(service, user) }
func (OSKeychain) Has(service, user string) bool            { return osTiered.Has(service, user) }
func (OSKeychain) Protected(service, user string) bool      { return osTiered.Protected(service, user) }
func (OSKeychain) Upgrade(service, user string, dek []byte) { osTiered.Upgrade(service, user, dek) }
