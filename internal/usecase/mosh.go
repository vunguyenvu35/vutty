package usecase

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

const moshPrefix = "mosh:"

// MoshID is the session host id that opens a saved host over Mosh instead of SSH.
func MoshID(hostID string) string { return moshPrefix + hostID }

// baseHostID is the saved host a session host id stands for: Telnet and Mosh ids carry the id of the host behind a prefix.
func baseHostID(id string) string {
	return strings.TrimPrefix(strings.TrimPrefix(id, telnetPrefix), moshPrefix)
}

// moshServerCmd starts a mosh-server on the host for one client; -s binds it to the address the SSH login came from. The first
// locale is the one most hosts have; the second is for those that have only C.UTF-8 (mosh-server insists on a UTF-8 locale).
const moshServerCmd = `command -v mosh-server >/dev/null 2>&1 || { echo "mosh-server is not installed on this host" >&2; exit 127; }
mosh-server new -s -c 256 -l LANG=en_US.UTF-8 2>&1 || mosh-server new -s -c 256 -l LANG=C.UTF-8 2>&1`

var moshConnect = regexp.MustCompile(`(?m)^MOSH CONNECT (\d{1,5}) ([A-Za-z0-9/+]{22})\s*$`)

// parseMoshConnect reads the port and the key from what mosh-server printed.
func parseMoshConnect(out string) (port int, key string, err error) {
	m := moshConnect.FindStringSubmatch(out)
	if m == nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = "mosh-server did not answer"
		}
		return 0, "", errors.New(cut(msg, 300))
	}
	port, _ = strconv.Atoi(m[1])
	if port < 1 || port > 65535 {
		return 0, "", errors.New("mosh-server gave a port that is not a port")
	}
	return port, m[2], nil
}

// moshIP is the address mosh-client is given: it takes an IP, not a name, so a name is looked up here (IPv4 first, as mosh does).
func moshIP(host string) (string, error) {
	if ip := net.ParseIP(host); ip != nil {
		return ip.String(), nil
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("cannot find the address of %s", host)
	}
	for _, ip := range ips {
		if ip.To4() != nil {
			return ip.String(), nil
		}
	}
	return ips[0].String(), nil
}

// openMosh does what the `mosh` command does: log in over SSH (with the same host key and sign-in questions as a terminal), start a
// mosh-server there, drop the SSH connection and start the Mosh client of this computer on the port and key it printed.
func (s *SessionService) openMosh(sess *session, cols, rows int, onData func([]byte)) (Shell, string, error) {
	hostID := strings.TrimPrefix(sess.hostID, moshPrefix)
	h, err := s.c.Store.GetHost(hostID)
	if err != nil {
		return nil, "", err
	}
	if !h.MoshEnabled {
		return nil, h.Label, errors.New("Mosh is not turned on for this host: add it in the host form")
	}
	if s.c.OpenMosh == nil {
		return nil, h.Label, errors.New("Mosh is not available in this build")
	}
	if h.JumpHostID != "" {
		return nil, h.Label, errors.New("Mosh cannot go through a jump host: its UDP packets need a direct route")
	}
	cl, _, err := s.c.connect(hostID, sess.sid, sess.abort)
	if err != nil {
		return nil, h.Label, err
	}
	out, rerr := runScript(cl, moshServerCmd, serverTimeout)
	_ = cl.Close() // the mosh-server goes on without it
	port, key, err := parseMoshConnect(out)
	if err != nil {
		if rerr != nil && out == "" {
			err = rerr
		}
		return nil, h.Label, err
	}
	ip, err := moshIP(h.Hostname)
	if err != nil {
		return nil, h.Label, err
	}
	sh, err := s.c.OpenMosh(MoshTarget{IP: ip, Port: port, Key: key}, cols, rows, onData)
	return sh, h.Label, err
}
