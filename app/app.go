// Package app is the delivery layer: it adapts the use cases to Wails bindings and events and
// is the composition root that builds the concrete adapters (storage, vault, keychain).
package app

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"vutty/internal/agentx"
	"vutty/internal/domain/assistant"
	"vutty/internal/ftpx"
	"vutty/internal/localtty"
	"vutty/internal/mcpgw"
	"vutty/internal/serialx"
	"vutty/internal/sftpx"
	"vutty/internal/sqlitedb"
	"vutty/internal/sshx"
	"vutty/internal/store"
	"vutty/internal/telnetx"
	"vutty/internal/usecase"
	"vutty/internal/vault"
)

// Version is injected at build time with -ldflags "-X vutty/app.Version=...".
var Version = "dev"

// App bundles the use cases (bound to the frontend) and the Wails-only system service.
type App struct {
	*usecase.Services
	System *SystemService
	db     *sqlitedb.DB
	dir    string

	bridge  *Bridge
	windows *WindowLauncher
	ctx     context.Context
}

// DataDir resolves where user data lives: $VUTTY_DATA_DIR, else the OS config dir
// (macOS: ~/Library/Application Support/vutty). The app was called VuTerm and, before that, VVTerm: their
// $VUTERM_DATA_DIR / $VVTERM_DATA_DIR and old folders are still honoured, and an old folder is renamed on first start.
func DataDir() (string, error) {
	for _, name := range []string{"VUTTY_DATA_DIR", "VUTERM_DATA_DIR", "VVTERM_DATA_DIR"} {
		if d := os.Getenv(name); d != "" {
			return d, nil
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return adoptLegacyDir(filepath.Join(base, "vutty"), filepath.Join(base, "vuterm"), filepath.Join(base, "vvterm")), nil
}

// adoptLegacyDir returns the folder to use: dir, after moving a pre-rename folder to it when only an old one
// exists (legacies are listed newest name first). If the move fails the old folder is used as it is, so
// nothing is ever left behind.
func adoptLegacyDir(dir string, legacies ...string) string {
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	for _, legacy := range legacies {
		if _, err := os.Stat(legacy); err != nil {
			continue
		}
		if err := os.Rename(legacy, dir); err != nil {
			return legacy
		}
		return dir
	}
	return dir
}

// adoptLegacyDB renames the pre-rename database files (vuterm.db or vvterm.db, and their journal files) next to path.
func adoptLegacyDB(path string) {
	if _, err := os.Stat(path); err == nil {
		return
	}
	for _, name := range []string{"vuterm.db", "vvterm.db"} {
		legacy := filepath.Join(filepath.Dir(path), name)
		if _, err := os.Stat(legacy); err != nil {
			continue
		}
		for _, suffix := range []string{"", "-wal", "-shm"} {
			_ = os.Rename(legacy+suffix, path+suffix)
		}
		return
	}
}

// keychainAccount is the key of this vault's entry in the OS keychain. It is the data folder's path as it was
// before the first rename (".../vvterm/data"), so Quick unlock keeps working for people who used VVTerm or VuTerm.
func keychainAccount(dir, data string) string {
	if b := filepath.Base(dir); b == "vutty" || b == "vuterm" {
		return filepath.Join(filepath.Dir(dir), "vvterm", "data")
	}
	return data
}

// Build is the composition root: it creates the concrete adapters and wires the use cases.
func Build(dir string, kc vault.Keychain, kdf *vault.KDFParams) (*App, error) {
	data := filepath.Join(dir, "data")
	if err := os.MkdirAll(data, 0o700); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(data, "vutty.db")
	adoptLegacyDB(dbPath)
	db, err := sqlitedb.Open(dbPath)
	if err != nil {
		return nil, err
	}
	st, err := store.OpenWith(db.StorePersister())
	if err != nil {
		db.Close()
		return nil, err
	}
	realKeychain := kc == nil
	if realKeychain {
		kc = vault.OSKeychain{}
	}
	v, err := vault.NewWith(db.VaultPersister(), keychainAccount(dir, data), kc)
	if err != nil {
		db.Close()
		return nil, err
	}
	if realKeychain && vault.BiometricAvailable() {
		v.SetAuthenticator(vault.BiometricAuth) // the keychain key needs Touch ID, not just a click
	}
	if kdf != nil {
		v.SetKDF(*kdf)
	}
	core, err := usecase.NewCore(dir, usecase.Options{CacheDir: cacheDir(), Store: st, Vault: v, OpenRemoteFS: func(c *sshx.Client) (usecase.RemoteFS, error) { return sftpx.Open(c.Client) },
		OpenFTP: func(cfg usecase.FTPConfig) (usecase.RemoteFS, error) {
			f, err := ftpx.Open(ftpx.Config{Addr: cfg.Addr, Security: cfg.Security, User: cfg.User, Password: cfg.Password})
			if err != nil {
				return nil, err
			}
			return f, nil
		},
		OpenLocalShell: func(cols, rows int, onData func([]byte)) (usecase.Shell, error) {
			return localtty.Open(cols, rows, onData)
		},
		OpenSerial: func(path string, baud int, onData func([]byte)) (usecase.Shell, error) {
			return serialx.Open(path, baud, onData)
		},
		OpenTelnet: func(addr string, cols, rows int, user, pass string, onData func([]byte)) (usecase.Shell, error) {
			return telnetx.Open(addr, 10*time.Second, cols, rows, user, pass, onData)
		},
		ListSerial: serialx.Ports, Activity: db.ActivityLog(),
		// AI assistant: the provider adapters, the MCP gateway and the connector for users' MCP servers
		Agents: []usecase.Agent{&agentx.Claude{}, &agentx.Codex{}, &agentx.Gemini{}, &agentx.Antigravity{}},
		NewCustomAgent: func(p assistant.CustomProvider) usecase.Agent {
			return &agentx.Custom{Cfg: agentx.CustomConfig{ID: p.ID, Name: p.Name, Command: p.Command, Args: p.Args, PromptVia: p.PromptVia}}
		},
		ToolGateway: mcpgw.New(), MCP: mcpgw.Connector{}, Kube: kubeConnector{}})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &App{Services: usecase.New(core), System: NewSystemService(), db: db, dir: dir}, nil
}

// Startup is the Wails OnStartup hook: it routes use case events to the frontend.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.System.SetMenuBuilder(a.menuWith)
	a.System.SetContext(ctx)
	a.Core.SetEmitter(func(event string, data ...any) {
		runtime.EventsEmit(ctx, event, data...)
		if a.bridge != nil {
			a.bridge.Publish(event, data)
		}
	})
	a.startSleepLock(ctx) // the vault locks when the computer sleeps
	a.System.updateDir = filepath.Join(a.dir, "updates")
	CleanUpAfterUpdate(a.System.updateDir) // what the last update left behind
	// The AI tools are looked for when something first asks for them (the assistant panel, Settings → AI assistant): starting
	// each CLI takes seconds of CPU, which would compete with the page that was opened right after the app started.

	// An app window (Duplicate in new window) is another process that talks to this one through the bridge.
	a.bridge = NewBridge(a, func(service string) {
		runtime.EventsEmit(ctx, "bridge:changed", service)
		a.bridge.Publish("bridge:changed", []any{service}) // the other app windows too
	})
	a.bridge.SetMenuHandler(func(action string) {
		runtime.WindowUnminimise(ctx)
		runtime.WindowShow(ctx)
		runtime.EventsEmit(ctx, MenuEvent, action)
	})
	if err := a.bridge.Start(a.dir); err == nil {
		a.windows = NewWindowLauncher()
		a.System.SetWindowOpener(a.windows.Open)
	}
}

// Shutdown is the Wails OnShutdown hook.
func (a *App) Shutdown(ctx context.Context) {
	if a.windows != nil {
		a.windows.Close()
	}
	if a.bridge != nil {
		a.bridge.Close()
	}
	a.Services.Shutdown()
	_ = a.db.Close()
}

// Menu is the application menu bar, wired to this window.
func (a *App) Menu() *menu.Menu {
	saved := ""
	if a.Services != nil && a.Settings != nil {
		saved = a.Settings.Get().Language
	}
	lang := ResolveMenuLang(saved) // the page tells the resolved language when it is up
	if a.System != nil {
		a.System.SetMenuLang(lang)
	}
	return a.menuWith(nil, lang)
}

func (a *App) menuWith(shortcuts map[string]string, lang string) *menu.Menu {
	return BuildMenu(MenuHooks{
		Shortcuts: shortcuts,
		Lang:      lang,
		Emit: func(action string) {
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, MenuEvent, action)
			}
		},
		OpenURL: func(url string) {
			if a.ctx != nil && urlAllowed(url) {
				runtime.BrowserOpenURL(a.ctx, url)
			}
		},
		ToggleFullScreen: func() {
			if a.ctx == nil {
				return
			}
			if runtime.WindowIsFullscreen(a.ctx) {
				runtime.WindowUnfullscreen(a.ctx)
			} else {
				runtime.WindowFullscreen(a.ctx)
			}
		},
	})
}

// cacheDir is where the app keeps files it can make again; empty when the OS has none.
func cacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "vutty")
}
