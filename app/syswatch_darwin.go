//go:build darwin

package app

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

extern void goSystemEvent(int kind);

// startSystemWatch asks macOS to say when the computer is about to sleep and when the screen is locked. The blocks run on the
// thread that posts the notification; Go does the rest. (The screen-locked notification comes through the distributed notification
// center, which needs the application's run loop: it works in the app, and no test of ours can run it.)
static void startSystemWatch(void) {
    NSNotificationCenter *ws = [[NSWorkspace sharedWorkspace] notificationCenter];
    [ws addObserverForName:NSWorkspaceWillSleepNotification object:nil queue:nil usingBlock:^(NSNotification *n) { goSystemEvent(0); }];
    [[NSDistributedNotificationCenter defaultCenter] addObserverForName:@"com.apple.screenIsLocked" object:nil queue:nil usingBlock:^(NSNotification *n) { goSystemEvent(1); }];
}

// postSleepNotification sends the notification the system sends before a sleep, for the test.
static void postSleepNotification(void) {
    [[[NSWorkspace sharedWorkspace] notificationCenter] postNotificationName:NSWorkspaceWillSleepNotification object:nil];
}
*/
import "C"

import "sync"

var (
	sysMu   sync.Mutex
	sysFunc func(reason string)
)

//export goSystemEvent
func goSystemEvent(kind C.int) {
	sysMu.Lock()
	f := sysFunc
	sysMu.Unlock()
	if f == nil {
		return
	}
	if kind == 0 {
		f("the computer went to sleep")
	} else {
		f("the screen was locked")
	}
}

// watchSystem calls f when macOS says the computer is going to sleep or the screen was locked.
func watchSystem(f func(reason string)) {
	sysMu.Lock()
	first := sysFunc == nil
	sysFunc = f
	sysMu.Unlock()
	if first {
		C.startSystemWatch()
	}
}

// simulateSleepNotification is for the test: the same notification macOS posts before a sleep.
func simulateSleepNotification() { C.postSleepNotification() }
