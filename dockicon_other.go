//go:build !darwin

package main

// setDockIcon is only needed on macOS.
func setDockIcon([]byte) {}

// nativeNotify is only used on macOS; the other systems use the program of internal/usecase.
var nativeNotify func(title, body string) error

// slimFullScreenBar is only needed on macOS.
func slimFullScreenBar() {}
