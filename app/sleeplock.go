package app

import (
	"context"
	"time"
)

// A computer that sleeps keeps the vault key in memory, and whoever opens the lid finds the vault open. These locks it when the
// computer goes to sleep (and, on a Mac, when the screen is locked), if the setting is on. Two things notice it:
//   - the system, where it says so before the sleep (macOS: see syswatch_darwin.go), which locks at once;
//   - a clock that jumped: the app looks at the time every few seconds, and a gap far longer than that means the computer was
//     asleep. That works everywhere, but only after it wakes, and not for a sleep of a few seconds.

const (
	sleepTick = 5 * time.Second
	// sleepGap is how long a pause between two ticks has to be to count as a sleep: ticks are 5 seconds apart, so a stall
	// of the app itself (a busy machine) does not come anywhere near it.
	sleepGap = 40 * time.Second
)

// gapDetector says when the time between two looks is longer than a running program could have waited.
type gapDetector struct {
	last time.Time
	gap  time.Duration
}

// Observe is called with the time at each tick; it reports a gap longer than the limit since the previous call. The wall clock is
// read without the monotonic part, which stops during a sleep on some systems.
func (g *gapDetector) Observe(now time.Time) bool {
	now = now.Round(0)
	slept := !g.last.IsZero() && now.Sub(g.last) > g.gap
	g.last = now
	return slept
}

// watchSleepGap calls onSleep after every tick that follows a long gap, until ctx ends.
func watchSleepGap(ctx context.Context, tick time.Duration, gap time.Duration, now func() time.Time, onSleep func()) {
	g := &gapDetector{gap: gap}
	g.Observe(now())
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if g.Observe(now()) {
				onSleep()
			}
		}
	}
}

// lockOnSleep locks the vault if it is open and the setting is on. reason goes into the Logs.
func (a *App) lockOnSleep(reason string) {
	if !a.Settings.Get().LockOnSleep {
		return
	}
	a.Vault.LockBecause(reason)
}

// startSleepLock starts what notices a sleep. It belongs to the main window's process, the one that holds the vault.
func (a *App) startSleepLock(ctx context.Context) {
	watchSystem(a.lockOnSleep)
	go watchSleepGap(ctx, sleepTick, sleepGap, time.Now, func() { a.lockOnSleep("the computer was asleep") })
}
