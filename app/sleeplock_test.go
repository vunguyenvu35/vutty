package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"vutty/internal/domain"
	"vutty/internal/vault"
)

func TestGapDetectorOnlySeesALongPauseBetweenTicks(t *testing.T) {
	g := &gapDetector{gap: 40 * time.Second}
	t0 := time.Date(2026, 11, 4, 9, 0, 0, 0, time.Local)
	if g.Observe(t0) {
		t.Fatal("the first look cannot be a gap")
	}
	for i := 1; i <= 5; i++ {
		if g.Observe(t0.Add(time.Duration(i) * 5 * time.Second)) {
			t.Fatal("a normal tick was taken for a sleep")
		}
	}
	if g.Observe(t0.Add(25*time.Second + 39*time.Second)) {
		t.Fatal("39 seconds is not a sleep")
	}
	if !g.Observe(t0.Add(25*time.Second + 39*time.Second + 41*time.Second)) {
		t.Fatal("41 seconds without a look is one")
	}
	if g.Observe(t0.Add(25*time.Second + 39*time.Second + 41*time.Second + 5*time.Second)) {
		t.Fatal("after waking, the next tick is normal again")
	}
	// a clock set back is no sleep
	if g.Observe(t0) {
		t.Fatal("time going backwards was taken for a sleep")
	}
}

func TestGapDetectorIgnoresTheMonotonicClock(t *testing.T) {
	g := &gapDetector{gap: 40 * time.Second}
	now := time.Now() // carries a monotonic reading, which can stand still while the computer sleeps
	g.Observe(now)
	later := now.Add(10 * time.Minute)
	if !g.Observe(later) {
		t.Fatal("the wall clock moved ten minutes: that is a sleep")
	}
}

func TestWatchSleepGapCallsBackOncePerSleepAndStopsWithItsContext(t *testing.T) {
	var calls atomic.Int32
	var n atomic.Int32
	base := time.Date(2026, 11, 4, 9, 0, 0, 0, time.Local)
	now := func() time.Time { // the third look is an hour later
		i := n.Add(1)
		if i >= 3 {
			return base.Add(time.Hour + time.Duration(i)*time.Millisecond)
		}
		return base.Add(time.Duration(i) * time.Millisecond)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watchSleepGap(ctx, 2*time.Millisecond, 40*time.Second, now, func() { calls.Add(1) })
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond) // more ticks, none of them a gap
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the watch did not stop")
	}
	if calls.Load() != 1 {
		t.Fatalf("called %d times, want once", calls.Load())
	}
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	a, err := Build(t.TempDir(), vault.NewMemKeychain(), &vault.KDFParams{Memory: 64, Time: 1, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Vault.Create("a long master password"); err != nil {
		t.Fatal(err)
	}
	return a
}

func lockLines(a *App) (n int, last string) {
	logs, _ := a.Log.List(domain.ActivityFilter{})
	for _, l := range logs {
		if l.Kind == domain.ActVault && len(l.Message) >= 12 && l.Message[:12] == "Vault locked" {
			if n == 0 {
				last = l.Message
			}
			n++
		}
	}
	return
}

func TestTheVaultLocksWhenTheComputerSleepsAndSaysWhy(t *testing.T) {
	a := newTestApp(t)
	if !a.Settings.Get().LockOnSleep {
		t.Fatal("locking on sleep must be on to begin with")
	}
	if !a.Vault.Status().Unlocked {
		t.Fatal("a new vault starts unlocked")
	}
	a.lockOnSleep("the computer went to sleep")
	if a.Vault.Status().Unlocked {
		t.Fatal("the vault stayed open")
	}
	if n, last := lockLines(a); n != 1 || last != "Vault locked: the computer went to sleep" {
		t.Fatalf("%d lines, last %q", n, last)
	}
	a.lockOnSleep("the screen was locked") // already locked: nothing more to say
	if n, _ := lockLines(a); n != 1 {
		t.Fatalf("a locked vault was locked again: %d lines", n)
	}
}

func TestTheVaultStaysOpenWhenTheSettingIsOff(t *testing.T) {
	a := newTestApp(t)
	in := a.Settings.Get()
	in.LockOnSleep = false
	if _, err := a.Settings.Save(in); err != nil {
		t.Fatal(err)
	}
	a.lockOnSleep("the computer went to sleep")
	if !a.Vault.Status().Unlocked {
		t.Fatal("the vault locked although the setting is off")
	}
	if n, _ := lockLines(a); n != 0 {
		t.Fatalf("%d lines", n)
	}
}
