package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	runtime2 "runtime"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// SystemService offers native dialogs and app info. It is the only place that touches the
// Wails runtime for dialogs.
type SystemService struct {
	ctx        context.Context
	openWindow func(hostID, title string) error
	menuFor    func(shortcuts map[string]string, lang string) *menu.Menu
	menuKeys   map[string]string
	menuLang   string // en|vi|ja|zh
	menuMu     sync.Mutex
	links      sshLinks
	updateDir  string // where an update is unpacked (inside the data folder)
}

func NewSystemService() *SystemService { return &SystemService{} }

// SetContext is called from OnStartup with the Wails context.
func (s *SystemService) SetContext(ctx context.Context) { s.ctx = ctx }

type AppInfo struct {
	Version string `json:"version"`
}

// SetMenuBuilder installs what makes this window's menu bar for a set of shortcuts and a language.
func (s *SystemService) SetMenuBuilder(f func(shortcuts map[string]string, lang string) *menu.Menu) {
	s.menuFor = f
}

// SetMenuLang sets the language the first menu bar is made in (before the page has told its own).
func (s *SystemService) SetMenuLang(lang string) {
	s.menuMu.Lock()
	s.menuLang = lang
	s.menuMu.Unlock()
}

// MenuLang is the language the menu bar is in now.
func (s *SystemService) MenuLang() string {
	s.menuMu.Lock()
	defer s.menuMu.Unlock()
	if s.menuLang == "" {
		return "en"
	}
	return s.menuLang
}

// applyMenu makes the menu bar again with the current shortcuts and language. Callers hold menuMu.
func (s *SystemService) applyMenu() {
	lang := s.menuLang
	if lang == "" {
		lang = "en"
	}
	runtime.MenuSetApplicationMenu(s.ctx, s.menuFor(s.menuKeys, lang))
	runtime.MenuUpdateApplicationMenu(s.ctx)
}

// SetMenuShortcuts is called by the page with the effective shortcuts (every action id to its key): the menu
// bar is made again so each item shows its key. It does nothing when the keys did not change.
func (s *SystemService) SetMenuShortcuts(shortcuts map[string]string) {
	if s.ctx == nil || s.menuFor == nil || runtime2.GOOS != "darwin" {
		return
	}
	s.menuMu.Lock()
	defer s.menuMu.Unlock()
	if len(shortcuts) == len(s.menuKeys) {
		same := true
		for k, v := range shortcuts {
			if s.menuKeys[k] != v {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	s.menuKeys = shortcuts
	s.applyMenu()
}

// SetLanguage is called by the page with the language it shows (en|vi|ja|zh, 'auto' already resolved): the menu
// bar is made again in that language, with the shortcuts it has now.
func (s *SystemService) SetLanguage(lang string) error {
	if !validMenuLang(lang) {
		return errors.New("unknown language")
	}
	s.menuMu.Lock()
	defer s.menuMu.Unlock()
	if s.menuLang == lang {
		return nil
	}
	s.menuLang = lang
	if s.ctx == nil || s.menuFor == nil || runtime2.GOOS != "darwin" {
		return nil
	}
	s.applyMenu()
	return nil
}

// SetWindowOpener installs what OpenWindow runs (the launcher of terminal window processes).
func (s *SystemService) SetWindowOpener(f func(hostID, title string) error) { s.openWindow = f }

// OpenWindow opens a new native window with a new session to the same place (a host, this computer, a
// serial port). It fails when there are no native windows, and the frontend says so.
func (s *SystemService) OpenWindow(hostID, title string) error {
	if s.noDialogs() || s.openWindow == nil {
		return errNoDialog
	}
	if hostID == "" {
		return errors.New("nothing to open")
	}
	return s.openWindow(hostID, title)
}

// riskyExt are files that run when they are opened: they are never opened from here, whatever their source.
var riskyExt = map[string]bool{
	".command": true, ".app": true, ".sh": true, ".bash": true, ".zsh": true, ".csh": true, ".pkg": true, ".dmg": true, ".workflow": true,
	".terminal": true, ".exe": true, ".bat": true, ".cmd": true, ".com": true, ".scr": true, ".msi": true, ".ps1": true, ".vbs": true,
	".jar": true, ".desktop": true, ".action": true, ".scpt": true, ".applescript": true,
}

func openCommand(path, with string) (*exec.Cmd, error) {
	if riskyExt[strings.ToLower(filepath.Ext(path))] {
		return nil, errors.New("this kind of file runs programs: it is not opened from here")
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() && runtime2.GOOS != "darwin" {
		return nil, errors.New("not a file that can be opened")
	}
	switch runtime2.GOOS {
	case "darwin":
		if with != "" {
			return exec.Command("open", "-a", with, path), nil
		}
		return exec.Command("open", path), nil
	case "windows":
		if with != "" {
			return exec.Command(with, path), nil
		}
		return exec.Command("cmd", "/c", "start", "", path), nil
	default:
		if with != "" {
			return exec.Command(with, path), nil
		}
		return exec.Command("xdg-open", path), nil
	}
}

// OpenPath opens a file with the application the system has for it.
func (s *SystemService) OpenPath(path string) error { return s.openFile(path, "") }

// OpenPathWith opens a file with the application chosen by the user (an app bundle or a program).
func (s *SystemService) OpenPathWith(path, app string) error { return s.openFile(path, app) }

func (s *SystemService) openFile(path, with string) error {
	if s.noDialogs() {
		return errNoDialog
	}
	if !filepath.IsAbs(path) {
		return errors.New("an absolute path is needed")
	}
	cmd, err := openCommand(path, with)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func (s *SystemService) Info() AppInfo { return AppInfo{Version: Version} }

var errNoDialog = errors.New("native dialogs are not available in this environment")

// noDialogs is true without a Wails context, or when VUTTY_NO_NATIVE_DIALOGS is set (the end-to-end
// run sets it: the UI then falls back to simple prompts that a browser test can answer).
func (s *SystemService) noDialogs() bool {
	return s.ctx == nil || os.Getenv("VUTTY_NO_NATIVE_DIALOGS") != ""
}

func (s *SystemService) PickFile(title string) (string, error) {
	if s.noDialogs() {
		return "", errNoDialog
	}
	return runtime.OpenFileDialog(s.ctx, runtime.OpenDialogOptions{Title: title, ShowHiddenFiles: true})
}

func (s *SystemService) PickDirectory(title string) (string, error) {
	if s.noDialogs() {
		return "", errNoDialog
	}
	return runtime.OpenDirectoryDialog(s.ctx, runtime.OpenDialogOptions{Title: title, CanCreateDirectories: true})
}

func (s *SystemService) SaveFile(title, defaultName string) (string, error) {
	if s.noDialogs() {
		return "", errNoDialog
	}
	return runtime.SaveFileDialog(s.ctx, runtime.SaveDialogOptions{Title: title, DefaultFilename: defaultName})
}

// maxClipboard bounds what is moved through the clipboard in one go.
const maxClipboard = 10 << 20

// ClipboardText returns the text on the system clipboard (the terminal's paste). It fails where there is no
// native window; the frontend then asks the browser instead.
func (s *SystemService) ClipboardText() (string, error) {
	if s.noDialogs() {
		return "", errNoDialog
	}
	t, err := runtime.ClipboardGetText(s.ctx)
	if len(t) > maxClipboard {
		t = t[:maxClipboard]
	}
	return t, err
}

// SetClipboardText puts text on the system clipboard (the terminal's copy).
func (s *SystemService) SetClipboardText(text string) error {
	if s.noDialogs() {
		return errNoDialog
	}
	if len(text) > maxClipboard {
		return errors.New("that is too much text for the clipboard")
	}
	return runtime.ClipboardSetText(s.ctx, text)
}

// allowedURLPrefixes are the only links the app opens in the browser (About page).
var allowedURLPrefixes = []string{
	"https://github.com/vunguyenvu35/vutty",
	"https://ko-fi.com/vunguyenvu",
}

func urlAllowed(u string) bool {
	for _, p := range allowedURLPrefixes {
		if u == p || strings.HasPrefix(u, p+"/") || strings.HasPrefix(u, p+"?") || strings.HasPrefix(u, p+"#") {
			return true
		}
	}
	return false
}

// OpenURL opens one of the project's own links in the default browser.
func (s *SystemService) OpenURL(url string) error {
	if !urlAllowed(url) {
		return errors.New("this link is not allowed")
	}
	if s.ctx == nil {
		return errNoDialog
	}
	runtime.BrowserOpenURL(s.ctx, url)
	return nil
}
