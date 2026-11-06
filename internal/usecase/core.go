// Package usecase holds the application logic. It depends on the domain and on the ports declared
// in ports.go, never on Wails or on concrete storage.

package usecase

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"vutty/internal/domain"
	"vutty/internal/fsutil"

	"vutty/internal/sshx"
)

// Settings is persisted in settings.json. Only the part listed in domain.Preferences is synced, and only while SyncPreferences is on.
type Settings struct {
	Theme         string `json:"theme"`          // app appearance: auto | light | dark
	TerminalTheme string `json:"terminal_theme"` // "auto" (follow the app) or a theme id from the UI catalogue
	FontSize      int    `json:"font_size"`
	FontFamily    string `json:"font_family"`
	CursorStyle   string `json:"cursor_style"` // block | bar | underline
	CursorBlink   bool   `json:"cursor_blink"`
	// CopyOnSelect copies the selection to the clipboard as soon as it is made.
	CopyOnSelect bool `json:"copy_on_select"`
	// RightClick is what a right click in a terminal does: paste | menu | none.
	RightClick      string `json:"right_click"`
	Scrollback      int    `json:"scrollback"`
	AutoLockMinutes int    `json:"auto_lock_minutes"`
	SyncURL         string `json:"sync_url"`
	SyncFolder      string `json:"sync_folder"` // synced-folder mode (Drive / iCloud / Dropbox); replaces SyncURL

	// General
	Zoom            int  `json:"zoom"`              // interface scale in percent (80..160)
	AskProtocol     bool `json:"ask_protocol"`      // a host opens on a "Choose protocol" screen first (off by default)
	ConfirmTabClose bool `json:"confirm_tab_close"` // ask before closing a terminal tab or pane (off by default)
	// ConfirmProd asks before text with a line break is pasted, or a snippet or a history command is run, in the terminal of
	// a host marked prod: a pasted newline runs the line at once (on by default).
	ConfirmProd bool `json:"confirm_prod"`
	// SessionLog writes what a terminal session printed to a text file: off (the default), prod (only hosts marked prod) or all.
	// SessionLogDir is the folder for them; empty means <data>/session-logs.
	SessionLog    string `json:"session_log"`
	SessionLogDir string `json:"session_log_dir"`
	// LockOnSleep locks the vault when the computer goes to sleep, and on a Mac when the screen is locked (on by default).
	LockOnSleep bool `json:"lock_on_sleep"`
	// Language is the language of the interface: auto (the language of the system) | en | vi | ja | zh
	Language  string `json:"language"`
	Highlight bool   `json:"highlight"` // colour URLs, IPs, paths and error/warning/success words in terminal output
	// GpuRenderer draws the terminal that has the focus with WebGL, which stays fast on a screen redrawn with every cell coloured (on by default)
	GpuRenderer  bool `json:"gpu_renderer"`
	ReduceMotion bool `json:"reduce_motion"` // turn animations off
	// AutoUpdateCheck: look for a newer release on GitHub when the app opens, at most once a day (on by default)
	AutoUpdateCheck bool `json:"auto_update_check"`
	// BiometricOnOpen: the lock screen asks for Touch ID / Windows Hello by itself (on by default)
	BiometricOnOpen bool `json:"biometric_on_open"`
	// Suggest shows a command suggestion while typing in a terminal (on by default); SuggestHost also reads the
	// shell history of an SSH host for it (off by default: it opens a file on the server).
	Suggest     bool `json:"suggest"`
	SuggestHost bool `json:"suggest_host"`

	// Account: a local profile today, the hook for sign-in and team features later
	AccountName  string `json:"account_name"`
	AccountEmail string `json:"account_email"`

	// Shortcuts holds only the user's overrides (action id -> combo such as "Mod+Shift+K"); an action
	// without an entry uses its default, which lives with the UI that implements it.
	Shortcuts map[string]string `json:"shortcuts"`

	// SyncWhat is what the user chose to sync (all by default): the kinds of data (hosts, keys, forwards, snippets) and the
	// preference groups (see prefGroups). Of the preferences, the values go into the sync
	// bundle and newer ones are taken from it. PrefsUpdated is when each group was last changed here or taken from the bundle;
	// the UI cannot set it.
	SyncWhat     []string             `json:"sync_what"`
	PrefsUpdated map[string]time.Time `json:"prefs_updated"`
}

func defaultSettings() Settings {
	return Settings{Theme: "dark", TerminalTheme: "auto", FontSize: 13, FontFamily: "Menlo, Monaco, 'Courier New', monospace",
		CursorStyle: "block", CursorBlink: true, CopyOnSelect: true, RightClick: "paste", Scrollback: 10000, AutoLockMinutes: 15,
		Language: "auto", Zoom: 100, ConfirmProd: true, LockOnSleep: true, SessionLog: SessionLogOff, Highlight: true, GpuRenderer: true, AskProtocol: true, BiometricOnOpen: true, AutoUpdateCheck: true, Suggest: true, SyncWhat: allSyncWhat(), Shortcuts: map[string]string{}}
}

// Core wires the packages together; the services below share one instance.
type Core struct {
	CacheDir string // files that can be made again; see SettingsService.ClearCache
	Dir      string // root of user data (contains data/ and sync/)
	DataDir  string
	SyncDir  string
	Store    Repository
	Vault    SecretVault
	Dialer   *sshx.Dialer
	// OpenRemoteFS starts SFTP on an SSH connection; nil disables the SFTP feature.
	OpenRemoteFS RemoteFSOpener
	// OpenLocalShell, OpenSerial and ListSerial back the Terminal and Serial features; nil disables them.
	OpenLocalShell ShellOpener
	OpenSerial     SerialOpener
	OpenTelnet     TelnetOpener
	OpenFTP        FTPOpener
	OpenRDP        RDPLauncher
	OpenMosh       MoshOpener
	ListSerial     SerialLister
	// Activity is the log shown on the Logs page; nil keeps no history.
	Activity ActivityLog
	// AI assistant adapters.
	Agents         []Agent
	NewCustomAgent CustomAgentFactory
	ToolGateway    ToolGateway
	MCP            MCPConnector
	// Kube opens clusters from a kubeconfig; nil turns the feature off.
	Kube KubeConnector
	// kubeShells are the pods that terminal tabs ("kubeexec:<id>") open a shell in.
	kubeShells sync.Map
	emit       Emitter
	setMu      sync.Mutex
	settings   Settings

	prompts     *prompter
	authPrompts *authPrompter
}

// Options carries the adapters and settings NewCore needs. The composition root builds the
// concrete Repository and SecretVault (see app.NewCore); tests inject their own.
type Options struct {
	Store Repository
	// CacheDir is where the app keeps files it can make again (optional: without it Clear cache only drops what is in memory).
	CacheDir string
	Vault    SecretVault
	Emit     Emitter
	// OpenRemoteFS is the SFTP adapter (optional).
	OpenRemoteFS RemoteFSOpener
	// Local terminal and serial adapters (optional).
	OpenLocalShell ShellOpener
	OpenSerial     SerialOpener
	OpenTelnet     TelnetOpener
	OpenFTP        FTPOpener
	OpenRDP        RDPLauncher
	OpenMosh       MoshOpener
	ListSerial     SerialLister
	// Activity stores the activity log (optional).
	Activity ActivityLog
	// AI assistant adapters (all optional: without Agents the assistant lists no providers).
	Agents         []Agent
	NewCustomAgent CustomAgentFactory
	ToolGateway    ToolGateway
	MCP            MCPConnector
	// Kube opens clusters from a kubeconfig (optional: without it the Clusters feature is off).
	Kube KubeConnector
}

// NewCore wires the use cases around the given ports. dir is the root of user data.
func NewCore(dir string, opt Options) (*Core, error) {
	if opt.Store == nil || opt.Vault == nil {
		return nil, errors.New("store and vault ports are required")
	}
	c := &Core{CacheDir: opt.CacheDir, Dir: dir, DataDir: filepath.Join(dir, "data"), SyncDir: filepath.Join(dir, "sync"), emit: opt.Emit,
		Store: opt.Store, Vault: opt.Vault, prompts: newPrompter(), authPrompts: newAuthPrompter(), OpenRemoteFS: opt.OpenRemoteFS,
		OpenLocalShell: opt.OpenLocalShell, OpenSerial: opt.OpenSerial, OpenTelnet: opt.OpenTelnet, OpenFTP: opt.OpenFTP, OpenRDP: opt.OpenRDP, OpenMosh: opt.OpenMosh, ListSerial: opt.ListSerial, Activity: opt.Activity,
		Agents: opt.Agents, NewCustomAgent: opt.NewCustomAgent, ToolGateway: opt.ToolGateway, MCP: opt.MCP, Kube: opt.Kube}
	if c.emit == nil {
		c.emit = func(string, ...any) {}
	}
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return nil, err
	}
	c.settings = defaultSettings()
	if b, err := os.ReadFile(filepath.Join(c.DataDir, "settings.json")); err == nil {
		_ = json.Unmarshal(b, &c.settings)
	}
	c.Vault.SetAutoLock(time.Duration(c.settings.AutoLockMinutes) * time.Minute)
	c.Vault.OnLock(func() { c.emit("vault:locked") })
	c.Dialer = &sshx.Dialer{
		HostKeys: &sshx.HostKeyChecker{Path: filepath.Join(c.DataDir, "known_hosts")},
		Timeout:  15 * time.Second,
	}
	return c, nil
}

// SetEmitter replaces the event sink once the delivery layer is ready (Wails OnStartup).
func (c *Core) SetEmitter(e Emitter) { c.emit = e }

func (c *Core) saveSettings() error {
	b, err := json.MarshalIndent(c.settings, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(c.DataDir, "settings.json"), b, 0o600)
}

var errNotUnlocked = errors.New("The vault is locked. Unlock it first.")

// prompter pairs a pending host-key question with its answer.
type prompter struct {
	mu      sync.Mutex
	pending map[string]chan bool
	fp      map[string]string
	pay     map[string]any
}

func newPrompter() *prompter {
	return &prompter{pending: map[string]chan bool{}, fp: map[string]string{}, pay: map[string]any{}}
}

// ask registers a question under key and returns the channel the answer arrives on.
func (p *prompter) ask(key, fingerprint string, payload any) chan bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch := make(chan bool, 1)
	p.pending[key] = ch
	p.fp[key] = fingerprint
	p.pay[key] = payload
	return ch
}

// payload returns the pending prompt's event payload, for replay on Attach.
func (p *prompter) payload(key string) (any, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.pay[key]
	return v, ok
}

func (p *prompter) clear(key string) {
	p.mu.Lock()
	delete(p.pending, key)
	delete(p.fp, key)
	delete(p.pay, key)
	p.mu.Unlock()
}

// answer delivers the user's decision. The fingerprint must match the one that was shown,
// so a stale or forged answer cannot trust a different key.
func (p *prompter) answer(key, fingerprint string, trust bool) error {
	p.mu.Lock()
	ch, ok := p.pending[key]
	want := p.fp[key]
	p.mu.Unlock()
	if !ok {
		return errors.New("no host key confirmation is pending")
	}
	if trust && fingerprint != want {
		return errors.New("fingerprint does not match the one that was displayed")
	}
	select {
	case ch <- trust:
	default:
	}
	return nil
}

// authAnswer is what the user gave to a keyboard-interactive round: one answer per question, or ok false when they
// cancelled.
type authAnswer struct {
	answers []string
	ok      bool
}

// authPrompter pairs a pending keyboard-interactive round (a verification code…) with its answer, like prompter does for
// a host key.
type authPrompter struct {
	mu      sync.Mutex
	pending map[string]chan authAnswer
	pay     map[string]any
}

func newAuthPrompter() *authPrompter {
	return &authPrompter{pending: map[string]chan authAnswer{}, pay: map[string]any{}}
}

func (p *authPrompter) ask(key string, payload any) chan authAnswer {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch := make(chan authAnswer, 1)
	p.pending[key], p.pay[key] = ch, payload
	return ch
}

// payload returns the pending round's event payload, for replay on Attach.
func (p *authPrompter) payload(key string) (any, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.pay[key]
	return v, ok
}

func (p *authPrompter) clear(key string) {
	p.mu.Lock()
	delete(p.pending, key)
	delete(p.pay, key)
	p.mu.Unlock()
}

// answer delivers the user's answers; nil answers mean they cancelled.
func (p *authPrompter) answer(key string, answers []string) error {
	p.mu.Lock()
	ch, ok := p.pending[key]
	p.mu.Unlock()
	if !ok {
		return errors.New("no question is waiting for an answer")
	}
	select {
	case ch <- authAnswer{answers: answers, ok: answers != nil}:
	default:
	}
	return nil
}

// learnOS works out the server type on the first connection to a host and stores it, so the UI can show
// the right icon. It runs in the background, asks for nothing the user typed, and gives up quietly when
// the server refuses commands (the SSH version string alone may still tell). It never runs again once
// the type is known.
func (c *Core) learnOS(host domain.Host, cl *sshx.Client) {
	if host.OS != "" {
		return
	}
	go func() {
		version := string(cl.Client.ServerVersion())
		out, _ := cl.Exec("uname -s; echo ---VUTTY---; cat /etc/os-release 2>/dev/null", 5*time.Second) // a non-zero exit still carries the output
		uname, release, _ := strings.Cut(out, "---VUTTY---")
		name := domain.ParseOS(version, uname, release)
		if name == "" {
			return
		}
		if err := c.Store.SetHostOS(host.ID, name); err == nil {
			c.emit("hosts:changed", host.ID)
		}
	}()
}
