package usecase

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vutty/internal/domain"
)

// rdpFile is the text of a .rdp file for a host: where to connect and as whom. It holds no password: the client asks for it.
func rdpFile(host string, port int, user string) (string, error) {
	for _, v := range []string{host, user} {
		if strings.ContainsAny(v, "\r\n\x00") {
			return "", errors.New("the host name or user name has a character that cannot be sent to the Remote Desktop client")
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "full address:s:%s:%d\r\n", host, port)
	if user != "" {
		fmt.Fprintf(&b, "username:s:%s\r\n", user)
	}
	b.WriteString("prompt for credentials:i:1\r\nauthentication level:i:2\r\n")
	return b.String(), nil
}

// OpenRDP starts the Remote Desktop client of this computer on a host that has RDP turned on. Vutty does not draw the desktop and
// keeps no RDP password. The .rdp file it writes is removed after a short while.
func (s *HostService) OpenRDP(hostID string) error {
	if s.c.OpenRDP == nil {
		return errors.New("Remote Desktop is not available in this build")
	}
	h, err := s.c.Store.GetHost(hostID)
	if err != nil {
		return err
	}
	if !h.RDPEnabled {
		return fmt.Errorf("Remote Desktop is not enabled for %q: edit the host and turn it on", h.Label)
	}
	if h.JumpHostID != "" {
		return errors.New("Remote Desktop cannot go through a jump host from here: the client connects directly")
	}
	user := h.RDPUsername
	if user == "" {
		user = h.Username
	}
	text, err := rdpFile(h.Hostname, h.RDPPort, user)
	if err != nil {
		return err
	}
	base := s.c.CacheDir
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "rdp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "host-*.rdp")
	if err != nil {
		return err
	}
	name := f.Name()
	_, werr := f.WriteString(text)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(name)
		return werr
	}
	time.AfterFunc(2*time.Minute, func() { _ = os.Remove(name) }) // the client has read it long before
	err = s.c.OpenRDP(RDPSpec{Host: h.Hostname, Port: h.RDPPort, User: user, File: name})
	s.c.record(domain.ActConnect, h.ID, h.Label, rdpMessage(err), err == nil)
	return err
}

func rdpMessage(err error) string {
	if err != nil {
		return "Could not start the Remote Desktop client: " + err.Error()
	}
	return "Started the Remote Desktop client"
}
