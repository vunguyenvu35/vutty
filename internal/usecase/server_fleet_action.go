package usecase

import (
	"errors"
	"strings"
	"vutty/internal/domain"
)

// One thing done on one saved host without opening its panel, for the Fleet page to do on many hosts: look at the waiting updates
// (changes nothing), start installing them, or renew the certificates (certbot renews only the ones near their end, then the web server is reloaded). Each host
// gets a short connection of its own that never asks a question, as in FleetHost.

// ServerFleetResult is what one host answered.
type ServerFleetResult struct {
	HostID   string `json:"host_id"`
	Error    string `json:"error"` // empty when the host answered and the action worked
	Output   string `json:"output"`
	Manager  string `json:"manager"`  // updates: apt, dnf, yum or ""
	Count    int    `json:"count"`    // updates waiting
	Security int    `json:"security"` // of them, security updates
	Reboot   bool   `json:"reboot_required"`
}

var fleetActions = map[string]bool{"updates": true, "renew": true, "install": true}

const fleetRenewScript = `command -v certbot >/dev/null 2>&1 || { echo "certbot is not installed on this server" >&2; exit 1; }
$SUDO certbot renew --non-interactive 2>&1
RC=$?
if [ $RC -eq 0 ]; then $SUDO systemctl reload nginx 2>/dev/null || $SUDO systemctl reload caddy 2>/dev/null; fi
exit $RC`

// FleetAction does "updates" or "renew" on one saved host. The host not answering, or the action failing, is no Go error: it is in
// Error. Only an unknown host or an unknown action is.
func (s *ServerService) FleetAction(hostID, action string) (ServerFleetResult, error) {
	if !fleetActions[action] {
		return ServerFleetResult{}, errors.New("unknown action")
	}
	h, err := s.c.Store.GetHost(hostID)
	if err != nil {
		return ServerFleetResult{}, err
	}
	res := ServerFleetResult{HostID: hostID}
	cl, _, err := s.c.connectQuiet(hostID)
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	defer cl.Close()
	switch action {
	case "updates":
		out, err := runScript(cl, updatesScript, serverLongTimeout)
		if err != nil && strings.TrimSpace(out) == "" {
			res.Error = err.Error()
			return res, nil
		}
		u := parseUpdates(out)
		res.Manager, res.Count, res.Security, res.Reboot = u.Manager, u.Count, u.Security, u.RebootRequired
	case "install":
		// started in the background on that server; its panel shows how far it is
		_, err := runScript(cl, upgradeStartScript(), serverTimeout)
		if err != nil {
			res.Error = lastLine(err.Error())
		} else {
			res.Output = "started"
		}
		s.c.record(domain.ActConnect, hostID, h.Label, "install the package updates (fleet)", err == nil)
	case "renew":
		out, err := runScript(cl, asRoot+fleetRenewScript, certbotTimeout)
		res.Output = dockerTail(out, err)
		if err != nil {
			res.Error = lastLine(err.Error())
		}
		s.c.record(domain.ActConnect, hostID, h.Label, "renew the certificates (fleet)", err == nil)
	}
	return res, nil
}

// lastLine is the last non-empty line of a text.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
