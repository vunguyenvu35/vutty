//go:build darwin

package app

import (
	"testing"
	"time"
)

// The notification macOS posts before a sleep reaches the program (posted here by hand: a test cannot put the Mac to sleep).
func TestTheSystemSleepNotificationReachesTheProgram(t *testing.T) {
	got := make(chan string, 4)
	watchSystem(func(reason string) { got <- reason })
	simulateSleepNotification()
	select {
	case r := <-got:
		if r != "the computer went to sleep" {
			t.Fatalf("%q", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the sleep notification never arrived")
	}
}

func TestTheSleepLockIsTiedToTheSystemNotification(t *testing.T) {
	a := newTestApp(t)
	a.startSleepLock(t.Context())
	simulateSleepNotification()
	deadline := time.Now().Add(3 * time.Second)
	for a.Vault.Status().Unlocked && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if a.Vault.Status().Unlocked {
		t.Fatal("the vault was not locked by the sleep notification")
	}
}
