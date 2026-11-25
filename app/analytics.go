package app

import (
	"context"
	"os"
	"runtime"
	"time"

	"vutty/internal/analytics"
)

// analyticsApp names this app in the Google Analytics stream it shares with the owner's other apps.
const analyticsApp = "vutty"

// reportOpen sends the "app_open" event once per start of the app, and only when this build has a stream and the user
// said yes in Settings → General. It runs in the main process only: a terminal window is the same app, not another
// start. A failure (offline, blocked) is ignored: statistics never get in the way of the app.
func (a *App) reportOpen(ctx context.Context) {
	c := analytics.New()
	if c == nil {
		return
	}
	if u := os.Getenv("VUTTY_ANALYTICS_URL"); u != "" {
		c.URL = u // tests and screenshots point this at a local stand-in for Google
	}
	in := a.Settings.Get()
	if in.AnalyticsConsent != "yes" || in.AnalyticsID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = c.Send(ctx, analytics.Event{Name: "app_open", ClientID: in.AnalyticsID, App: analyticsApp, Version: Version, OS: runtime.GOOS, Arch: runtime.GOARCH})
}
