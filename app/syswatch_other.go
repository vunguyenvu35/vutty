//go:build !darwin

package app

// watchSystem has nothing to listen to where the system does not announce a sleep to a program; the clock watch (sleeplock.go)
// still notices one after the computer wakes.
func watchSystem(f func(reason string)) {}
