//go:build darwin

package app

import (
	"sync/atomic"
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

// The handler may tell the page (the vault locks, a line is logged), and Wails' ExecJS must not run on the thread that macOS
// posted the notification on: there it dies in the run loop (see goSystemEvent). The handler runs on another thread, and the
// notification does not return before the handler has finished (the vault is locked before the computer sleeps).
func TestTheHandlerOfASystemNotificationDoesNotRunOnTheThreadThatPostedIt(t *testing.T) {
	var onThread atomic.Uint64
	var finished atomic.Bool
	watchSystem(func(string) {
		onThread.Store(currentThreadID())
		time.Sleep(50 * time.Millisecond) // the notification waits for this
		finished.Store(true)
	})
	simulateSleepNotification()
	if !finished.Load() {
		t.Fatal("the notification returned before the handler had finished")
	}
	if onThread.Load() == 0 || callbackThread.Load() == 0 {
		t.Fatalf("not measured: handler %d, notification %d", onThread.Load(), callbackThread.Load())
	}
	if onThread.Load() == callbackThread.Load() {
		t.Fatal("the handler ran on the thread that macOS posted the notification on")
	}
}
