//go:build !darwin

package vault

// OSKeychain stores the DEK in Windows Credential Manager / Secret Service as an ordinary item.
type OSKeychain = plainKeychain
