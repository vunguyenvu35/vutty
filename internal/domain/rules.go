package domain

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

// serverPathRe is the shape of a folder the Panel may look at on a server: absolute, nothing a shell would read.
var serverPathRe = regexp.MustCompile(`^/[A-Za-z0-9_./-]{0,200}$`)

// ValidServerPath reports whether p is an absolute path of letters, digits and . _ - / without a ".." step.
func ValidServerPath(p string) bool {
	return serverPathRe.MatchString(p) && !strings.Contains(p, "..")
}

// MaxJumpDepth bounds jump host chains to avoid loops.
const MaxJumpDepth = 8

// ErrNotFound is returned when a referenced record does not exist.
var ErrNotFound = errors.New("record not found")

// ErrTransferDiscarded is the cancellation cause of a transfer that was cancelled for good: its partial
// file is deleted. A transfer stopped any other way (Pause, a lost connection) keeps it to continue from.
var ErrTransferDiscarded = errors.New("transfer cancelled")

// IsLoopback reports whether addr is a loopback IP or "localhost".
func IsLoopback(addr string) bool {
	if strings.EqualFold(addr, "localhost") {
		return true
	}
	ip := net.ParseIP(addr)
	return ip != nil && ip.IsLoopback()
}

// NormalizeHost validates a host and fills defaults (label, port, keepalive).
// Hostname and username are checked for whitespace/control characters because they end up in
// exported ssh config files, where a newline would inject extra directives.
func NormalizeHost(h *Host) error {
	if strings.TrimSpace(h.Hostname) == "" {
		return errors.New("hostname is required")
	}
	if strings.ContainsAny(h.Hostname+h.Username, " \t\r\n\x00") {
		return errors.New("hostname and username must not contain whitespace or control characters")
	}
	if h.Label == "" {
		h.Label = h.Hostname
	}
	if h.Port == 0 {
		h.Port = 22
	}
	if h.Port < 1 || h.Port > 65535 {
		return errors.New("port out of range")
	}
	switch h.Env {
	case "", EnvDev, EnvStaging, EnvProd:
	default:
		return fmt.Errorf("invalid env %q", h.Env)
	}
	if h.KeepaliveSec == 0 {
		h.KeepaliveSec = 30
	}
	if h.OS != "" && !KnownOS[h.OS] {
		return fmt.Errorf("invalid os %q", h.OS)
	}
	if h.TelnetEnabled && h.TelnetPort == 0 {
		h.TelnetPort = 23
	}
	if h.TelnetPort < 0 || h.TelnetPort > 65535 {
		return errors.New("telnet port out of range")
	}
	if !h.TelnetEnabled {
		h.TelnetPort = 0 // a port without Telnet means nothing
	}
	if len(h.TelnetUsername) > 128 || strings.ContainsAny(h.TelnetUsername, "\r\n\x00") {
		return errors.New("invalid telnet user name")
	}
	h.FTPSecurity = strings.ToLower(strings.TrimSpace(h.FTPSecurity))
	switch h.FTPSecurity {
	case "", "explicit", "implicit":
	default:
		return fmt.Errorf("invalid ftp security %q", h.FTPSecurity)
	}
	if h.FTPEnabled && h.FTPPort == 0 {
		h.FTPPort = 21
		if h.FTPSecurity == "implicit" {
			h.FTPPort = 990
		}
	}
	if h.FTPPort < 0 || h.FTPPort > 65535 {
		return errors.New("ftp port out of range")
	}
	if !h.FTPEnabled {
		h.FTPPort, h.FTPSecurity = 0, "" // a port or a mode without FTP means nothing
	}
	if len(h.FTPUsername) > 128 || strings.ContainsAny(h.FTPUsername, "\r\n\x00") {
		return errors.New("invalid ftp user name")
	}
	if h.RDPEnabled && h.RDPPort == 0 {
		h.RDPPort = 3389
	}
	if h.RDPPort < 0 || h.RDPPort > 65535 {
		return errors.New("rdp port out of range")
	}
	if !h.RDPEnabled {
		h.RDPPort = 0
	}
	if len(h.RDPUsername) > 128 || strings.ContainsAny(h.RDPUsername, "\r\n\x00") {
		return errors.New("invalid rdp user name")
	}
	if h.VNCEnabled && h.VNCPort == 0 {
		h.VNCPort = 5900
	}
	if h.VNCPort < 0 || h.VNCPort > 65535 {
		return errors.New("vnc port out of range")
	}
	if !h.VNCEnabled {
		h.VNCPort, h.VNCViaSSH = 0, false
	}
	switch h.TelnetEncoding = strings.ToLower(strings.TrimSpace(h.TelnetEncoding)); h.TelnetEncoding {
	case "", "utf-8", "iso-8859-1":
	default:
		return errors.New("unknown telnet encoding")
	}
	switch h.AIPolicy {
	case "", "off", "ask", "on":
	default:
		return fmt.Errorf("invalid assistant policy %q", h.AIPolicy)
	}
	if h.BackupPath != "" && !ValidServerPath(h.BackupPath) {
		return errors.New("invalid backup folder")
	}
	if len(h.TerminalTheme) > 40 || strings.ContainsAny(h.TerminalTheme, " \t\r\n\x00/\\") {
		return errors.New("invalid terminal theme")
	}
	return nil
}

// NormalizeForward validates a port forward and applies the safe default bind address.
// Binding anything but loopback requires AllowPublicBind to be set explicitly.
func NormalizeForward(f *PortForward) error {
	f.Label = strings.TrimSpace(f.Label)
	if len([]rune(f.Label)) > 80 || strings.ContainsAny(f.Label, "\r\n\x00") {
		return errors.New("label must be a single line of at most 80 characters")
	}
	switch f.Type {
	case ForwardLocal, ForwardRemote:
		if f.DestHost == "" || f.DestPort < 1 || f.DestPort > 65535 {
			return errors.New("destination host and port are required")
		}
	case ForwardDynamic:
	default:
		return fmt.Errorf("invalid forward type %q", f.Type)
	}
	if f.BindPort < 0 || f.BindPort > 65535 {
		return errors.New("bind port out of range")
	}
	if f.BindAddr == "" {
		f.BindAddr = "127.0.0.1"
	}
	if !f.AllowPublicBind && !IsLoopback(f.BindAddr) {
		return fmt.Errorf("binding to %s exposes the forward to the network: enable allow_public_bind explicitly", f.BindAddr)
	}
	return nil
}

// CheckJumpChain verifies that making `self` jump through `jump` cannot loop or exceed
// MaxJumpDepth. lookup returns a host by id.
func CheckJumpChain(self, jump string, lookup func(id string) (Host, bool)) error {
	seen := map[string]bool{}
	for id, n := jump, 0; id != ""; n++ {
		if id == self && self != "" {
			return errors.New("jump host chain forms a loop")
		}
		if seen[id] || n >= MaxJumpDepth {
			return errors.New("jump host chain too deep or looping")
		}
		seen[id] = true
		h, ok := lookup(id)
		if !ok {
			return fmt.Errorf("jump host: %w", ErrNotFound)
		}
		id = h.JumpHostID
	}
	return nil
}

// MaxSnippetBytes bounds a snippet script (it is typed into a terminal).
const MaxSnippetBytes = 64 * 1024

// NormalizeSnippet validates a snippet and trims its label. Scripts keep their exact text, but NUL
// bytes are rejected: they cannot be typed into a terminal and often hide garbage.
func NormalizeSnippet(s *Snippet) error {
	s.Label = strings.TrimSpace(s.Label)
	if s.Label == "" {
		return errors.New("snippet label is required")
	}
	if strings.TrimSpace(s.Script) == "" {
		return errors.New("snippet script is required")
	}
	if len(s.Script) > MaxSnippetBytes {
		return fmt.Errorf("snippet script is larger than %d KiB", MaxSnippetBytes/1024)
	}
	if strings.ContainsRune(s.Script, 0) {
		return errors.New("snippet script must not contain NUL characters")
	}
	return nil
}

// MaxActivityMessage bounds one log line.
const MaxActivityMessage = 500

// NormalizeActivity makes an entry safe to store: one line, bounded, with a known kind.
func NormalizeActivity(e *ActivityEntry) error {
	switch e.Kind {
	case ActConnect, ActDisconnect, ActHostKey, ActForward, ActSFTP, ActTransfer, ActVault, ActSync, ActAssistant:
	default:
		return fmt.Errorf("invalid activity kind %q", e.Kind)
	}
	e.Message = strings.Join(strings.Fields(strings.ReplaceAll(e.Message, "\x00", "")), " ")
	if r := []rune(e.Message); len(r) > MaxActivityMessage {
		e.Message = string(r[:MaxActivityMessage-1]) + "…"
	}
	if e.Message == "" {
		return errors.New("an activity needs a message")
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	return nil
}

// NormalizeCluster validates a cluster record and trims its label.
func NormalizeCluster(c *Cluster) error {
	c.Label = strings.TrimSpace(c.Label)
	if c.Label == "" {
		return errors.New("cluster name is required")
	}
	if len(c.Label) > 100 {
		return errors.New("cluster name is too long")
	}
	switch c.Env {
	case "", EnvProd, EnvStaging, EnvDev:
	default:
		return errors.New("unknown environment")
	}
	return nil
}
