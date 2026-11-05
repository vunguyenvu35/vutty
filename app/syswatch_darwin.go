//go:build darwin

package app

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <pthread.h>

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

static unsigned long long currentThread(void) { return (unsigned long long)(uintptr_t)pthread_self(); }
*/
import "C"

import (
	"sync"
	"sync/atomic"
)

var (
	sysMu   sync.Mutex
	sysFunc func(reason string)
)

// callbackThread is the thread macOS called goSystemEvent on (a test compares it with the thread of the handler).
var callbackThread atomic.Uint64

func currentThreadID() uint64 { return uint64(C.currentThread()) }

//export goSystemEvent
func goSystemEvent(kind C.int) {
	sysMu.Lock()
	f := sysFunc
	sysMu.Unlock()
	if f == nil {
		return
	}
	reason := "the screen was locked"
	if kind == 0 {
		reason = "the computer went to sleep"
	}
	callbackThread.Store(currentThreadID())
	// This runs on the thread that posted the notification: the main thread, inside a pass of the run loop that has an autorelease
	// pool. What f does tells the page (the vault locks, a line is written to the log), and Wails' ExecJS releases the string it was
	// given, which it does not own: on a thread with a pool that is one release too many, and the app dies in the run loop
	// (SIGSEGV in the block that ExecJS queues). So f runs on a goroutine, which is never on this thread, and this waits for it: the
	// vault has to be locked before the computer goes to sleep.
	done := make(chan struct{})
	go func() {
		defer close(done)
		f(reason)
	}()
	<-done
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
