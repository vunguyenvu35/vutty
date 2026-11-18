//go:build darwin

package app

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>

// appsForFile lists, one path per line, the applications that can open the file: the one the system uses first, then the rest.
static char *appsForFile(const char *path) {
    @autoreleasepool {
        NSURL *u = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
        NSWorkspace *ws = [NSWorkspace sharedWorkspace];
        NSMutableArray<NSString *> *out = [NSMutableArray array];
        NSURL *def = [ws URLForApplicationToOpenURL:u];
        if (def) [out addObject:def.path];
        for (NSURL *a in [ws URLsForApplicationsToOpenURL:u]) {
            if (![out containsObject:a.path]) [out addObject:a.path];
        }
        return strdup([[out componentsJoinedByString:@"\n"] UTF8String]);
    }
}
*/
import "C"

import (
	"unsafe"
)

// installedAppsFor asks macOS which applications can open the file at path (the file is only used for its type).
func installedAppsFor(path string) []string {
	c := C.CString(path)
	defer C.free(unsafe.Pointer(c))
	r := C.appsForFile(c)
	defer C.free(unsafe.Pointer(r))
	return splitLines(C.GoString(r))
}
