package app

import (
	"context"
	"time"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Helpers for the window of another process (an app window, see terminal_window.go).

// quitWithMain ends this process when the main process stops answering.
func quitWithMain(ctx context.Context, c *BridgeClient) {
	go func() {
		misses := 0
		for ctx.Err() == nil {
			time.Sleep(2 * time.Second)
			if c.Ping() != nil {
				if misses++; misses >= 3 {
					runtime.Quit(ctx)
					return
				}
				continue
			}
			misses = 0
		}
	}()
}

// menuViaMain is the main window's menu bar for a window of another process: its items are done by the main
// window (through the bridge); only Full Screen and the Help links are done here.
func menuViaMain(c *BridgeClient, ctx func() context.Context, lang string) *menu.Menu {
	return BuildMenu(MenuHooks{
		Lang: lang,
		Emit: func(action string) { go func() { _ = c.MenuAction(action) }() },
		OpenURL: func(url string) {
			if x := ctx(); x != nil && urlAllowed(url) {
				runtime.BrowserOpenURL(x, url)
			}
		},
		ToggleFullScreen: func() {
			x := ctx()
			if x == nil {
				return
			}
			if runtime.WindowIsFullscreen(x) {
				runtime.WindowUnfullscreen(x)
			} else {
				runtime.WindowFullscreen(x)
			}
		},
	})
}
