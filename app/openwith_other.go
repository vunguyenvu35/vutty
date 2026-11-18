//go:build !darwin

package app

// installedAppsFor has no list outside macOS: the UI then asks for a program to open the file with.
func installedAppsFor(string) []string { return nil }
