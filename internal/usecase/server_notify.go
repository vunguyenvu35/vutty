package usecase

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// NativeNotify, when the app sets it (macOS), shows the notification as the app itself, with its icon.
var NativeNotify func(title, body string) error

// Notify shows a desktop notification of the operating system (the Fleet page asks for one when a server needs a look). The text goes
// to the program as an argument or an environment variable, never inside a script that is parsed.
func (s *ServerService) Notify(title, body string) error {
	title, body = clip(title, 80), clip(body, 240)
	if title == "" {
		return errors.New("a notification needs a title")
	}
	if NativeNotify != nil {
		return NativeNotify(title, body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "osascript", "-e", "on run argv", "-e", "display notification (item 1 of argv) with title (item 2 of argv)", "-e", "end run", body, title)
	case "linux":
		cmd = exec.CommandContext(ctx, "notify-send", "--app-name=Vutty", title, body)
	case "windows":
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
			`Add-Type -AssemblyName System.Windows.Forms; $n = New-Object System.Windows.Forms.NotifyIcon; $n.Icon = [System.Drawing.SystemIcons]::Information; $n.Visible = $true; $n.ShowBalloonTip(8000, $env:VUTTY_TITLE, $env:VUTTY_BODY, 'Info'); Start-Sleep -Seconds 9; $n.Dispose()`)
		cmd.Env = append(cmd.Environ(), "VUTTY_TITLE="+title, "VUTTY_BODY="+body)
		return cmd.Start() // the balloon stays up for a few seconds: do not wait for it
	default:
		return errors.New("desktop notifications are not available on this system")
	}
	return cmd.Run()
}

// clip trims a text to n runes without a line break.
func clip(s string, n int) string {
	s = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(s))
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
