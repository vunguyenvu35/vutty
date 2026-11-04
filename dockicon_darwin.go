//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// setDockIcon makes the running app wear the given PNG in the Dock and the app switcher. macOS keeps an
// icon cache per app bundle that survives a rebuild (so a changed logo can show up weeks late), and a
// binary started outside a bundle has none: setting it from the app itself is right in every case.
static void setDockIcon(const void *bytes, int length) {
    NSData *data = [NSData dataWithBytes:bytes length:length];
    NSImage *image = [[NSImage alloc] initWithData:data];
    if (image == nil) return;
    dispatch_async(dispatch_get_main_queue(), ^{ [NSApp setApplicationIconImage:image]; });
}

// slimFullScreenBar makes the bar macOS slides over the page in full screen low, with the app's name in it, like
// other Mac apps. Its height comes from the toolbar Wails adds to push the window buttons down in the normal
// window: the toolbar is taken off while the window is in full screen and put back when it leaves. Call it once,
// before wails.Run.
static NSToolbar *savedToolbar = nil;
static void slimFullScreenBar(void) {
    NSNotificationCenter *c = [NSNotificationCenter defaultCenter];
    [c addObserverForName:NSWindowWillEnterFullScreenNotification object:nil queue:nil usingBlock:^(NSNotification *n) {
        NSWindow *w = n.object;
        if (w.toolbar != nil) { savedToolbar = w.toolbar; w.toolbar = nil; }
        w.titleVisibility = NSWindowTitleVisible;
    }];
    [c addObserverForName:NSWindowDidExitFullScreenNotification object:nil queue:nil usingBlock:^(NSNotification *n) {
        NSWindow *w = n.object;
        w.titleVisibility = NSWindowTitleHidden;
        if (savedToolbar != nil) { w.toolbar = savedToolbar; savedToolbar = nil; }
    }];
}
*/
import "C"

import "unsafe"

func setDockIcon(png []byte) {
	if len(png) == 0 {
		return
	}
	C.setDockIcon(unsafe.Pointer(&png[0]), C.int(len(png)))
}

// slimFullScreenBar: see the C function. Call it once, before wails.Run.
func slimFullScreenBar() { C.slimFullScreenBar() }
