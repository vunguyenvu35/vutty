package main

import (
	"context"
	"embed"
	"log"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"vutty/app"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	dir, err := app.DataDir()
	if err != nil {
		log.Fatalf("data dir: %v", err)
	}
	if len(os.Args) > 3 && os.Args[1] == app.WindowFlag {
		runTerminalWindow(dir, os.Args[2], os.Args[3])
		return
	}
	a, err := app.Build(dir, nil, nil)
	if err != nil {
		log.Fatalf("open data: %v", err)
	}

	for _, arg := range os.Args[1:] { // Windows and Linux hand a clicked ssh:// link over as an argument
		if strings.HasPrefix(strings.ToLower(arg), "ssh://") {
			a.System.OpenSSHLink(arg)
		}
	}
	slimFullScreenBar() // a low bar with the app's name when macOS slides it over the page in full screen
	err = wails.Run(&options.App{
		Title:     "Vutty",
		Width:     1280,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 17, G: 19, B: 24, A: 1},
		OnStartup:        func(ctx context.Context) { a.Startup(ctx); setDockIcon(appIcon) },
		OnShutdown:       a.Shutdown,
		Menu:             a.Menu(),
		Mac: &mac.Options{
			TitleBar:  mac.TitleBarHiddenInset(),
			About:     &mac.AboutInfo{Title: "Vutty", Message: "SSH client with an encrypted vault.\nVersion " + app.Version, Icon: appIcon},
			OnUrlOpen: a.System.OpenSSHLink, // an ssh:// link: the window asks before it opens anything
		},
		Bind: []interface{}{
			a.Host, a.Vault, a.Session, a.Forward, a.Sync, a.ImportExp, a.Settings, a.Snippet, a.Sftp, a.Local, a.Serial, a.Log, a.KnownHost, a.Assistant, a.Exec, a.System,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// runTerminalWindow is an app window (Duplicate in new window): the whole app in another process with its own
// native window. It holds no data: every call goes to the main process, so the vault, the hosts and the
// sessions are the same ones; it opens the given host in its first tab.
func runTerminalWindow(dir, hostID, title string) {
	w, err := app.NewTerminalWindow(dir, hostID, title)
	if err != nil {
		log.Fatal(err)
	}
	slimFullScreenBar()
	err = wails.Run(&options.App{
		Title:            "Vutty",
		Width:            1280,
		Height:           800,
		MinWidth:         900,
		MinHeight:        600,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 17, G: 19, B: 24, A: 1},
		OnStartup:        func(ctx context.Context) { w.Startup(ctx); setDockIcon(appIcon) },
		OnShutdown:       w.Shutdown,
		Menu:             w.Menu(),
		Mac:              &mac.Options{TitleBar: mac.TitleBarHiddenInset()},
		Bind:             []interface{}{w, w.System},
	})
	if err != nil {
		log.Fatal(err)
	}
}
