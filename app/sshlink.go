package app

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// SSHLink is where an ssh:// link points: the three things needed to find a saved host or to start a new one. A password in
// the link, a path and a query are dropped: a link opened from a web page or a chat never carries a secret in and never
// connects by itself.
type SSHLink struct {
	Username string `json:"username"`
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
}

const (
	maxLinkHost = 253
	maxLinkUser = 64
)

// ParseSSHLink reads ssh://[user[:password]@]host[:port][/…] (a bracketed IPv6 address included). Anything else, or a name
// that could not be a host name or a user name, is refused.
func ParseSSHLink(raw string) (SSHLink, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 2048 || strings.ContainsAny(raw, "\x00\r\n\t ") {
		return SSHLink{}, errors.New("not an ssh link")
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "ssh") {
		return SSHLink{}, errors.New("not an ssh link")
	}
	host := u.Hostname()
	if host == "" || len(host) > maxLinkHost || strings.HasPrefix(host, "-") || !validLinkHost(host) {
		return SSHLink{}, errors.New("the link has no usable host name")
	}
	link := SSHLink{Hostname: host, Port: 22}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return SSHLink{}, errors.New("the link has an invalid port")
		}
		link.Port = n
	}
	if u.User != nil {
		name := u.User.Username()
		if len(name) > maxLinkUser || strings.HasPrefix(name, "-") || !validLinkUser(name) {
			return SSHLink{}, errors.New("the link has an invalid user name")
		}
		link.Username = name
	}
	return link, nil
}

// validLinkHost: a DNS name, an IPv4 address or an IPv6 address; nothing a shell or a terminal would read as more.
func validLinkHost(h string) bool {
	if ip := net.ParseIP(strings.SplitN(h, "%", 2)[0]); ip != nil {
		return true
	}
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func validLinkUser(n string) bool {
	for _, r := range n {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_', r == '@', r == '+':
		default:
			return false
		}
	}
	return true
}

// sshLinks holds the link that arrived before the window could do anything with it (the app was launched by the link, or the
// vault was locked), and tells the screen about every new one.
type sshLinks struct {
	mu      sync.Mutex
	pending *SSHLink
}

// OpenSSHLink is what the operating system calls with an ssh:// link: it is checked, kept until the screen takes it, and the
// screen is told. A link that is not valid is ignored.
func (s *SystemService) OpenSSHLink(raw string) {
	link, err := ParseSSHLink(raw)
	if err != nil {
		return
	}
	s.links.mu.Lock()
	s.links.pending = &link
	s.links.mu.Unlock()
	if s.ctx != nil {
		runtime.WindowUnminimise(s.ctx)
		runtime.WindowShow(s.ctx)
		runtime.EventsEmit(s.ctx, "app:open-ssh", link)
	}
}

// TakePendingSSHLink returns the link that is waiting (and forgets it); the zero value, with no host, when there is none.
func (s *SystemService) TakePendingSSHLink() SSHLink {
	s.links.mu.Lock()
	defer s.links.mu.Unlock()
	if s.links.pending == nil {
		return SSHLink{}
	}
	l := *s.links.pending
	s.links.pending = nil
	return l
}
