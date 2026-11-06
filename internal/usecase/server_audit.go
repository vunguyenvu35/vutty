package usecase

import (
	"strconv"
	"strings"
)

// ServerAudit is what the Security check needs beyond what the Security tab already reads (sshd, fail2ban, failed logins).
type ServerAudit struct {
	Firewall string   `json:"firewall"`     // ufw, firewalld, nft (on); ufw-off, firewalld-off, none
	AutoUpd  string   `json:"auto_updates"` // yes, no (installed, not scheduled), none (not installed)
	UID0     []string `json:"uid0"`         // accounts other than root with user id 0
	EmptyPW  []string `json:"empty_passwords"`
	PWKnown  bool     `json:"passwords_known"` // the login could read /etc/shadow
	NoPasswd int      `json:"nopasswd"`        // sudoers rules that need no password; -1 when unknown
	Reboot   bool     `json:"reboot_required"`
}

// ServerLogin is one line of `last`: a login that worked.
type ServerLogin struct {
	User   string `json:"user"`
	TTY    string `json:"tty"`
	From   string `json:"from"`
	When   string `json:"when"`
	Status string `json:"status"` // "still logged in", or how it ended
}

const auditScript = `if command -v ufw >/dev/null 2>&1; then
  if $SUDO ufw status 2>/dev/null | grep -q '^Status: active'; then echo fw=ufw; else echo fw=ufw-off; fi
elif command -v firewall-cmd >/dev/null 2>&1; then
  if firewall-cmd --state 2>/dev/null | grep -q running; then echo fw=firewalld; else echo fw=firewalld-off; fi
elif command -v nft >/dev/null 2>&1 && [ "$($SUDO nft list ruleset 2>/dev/null | grep -c chain)" -gt 0 ]; then echo fw=nft
else echo fw=none; fi
if dpkg -s unattended-upgrades >/dev/null 2>&1; then
  if systemctl is-enabled apt-daily-upgrade.timer >/dev/null 2>&1; then echo auto=yes; else echo auto=no; fi
elif rpm -q dnf-automatic >/dev/null 2>&1; then
  if systemctl is-enabled dnf-automatic.timer dnf-automatic-install.timer 2>/dev/null | grep -q enabled; then echo auto=yes; else echo auto=no; fi
else echo auto=none; fi
echo "uid0=$(awk -F: '$3==0 && $1!="root" {printf "%s ", $1}' /etc/passwd)"
if $SUDO test -r /etc/shadow; then echo "emptypw=$($SUDO awk -F: '$2=="" {printf "%s ", $1}' /etc/shadow)"; else echo emptypw=?; fi
if $SUDO test -r /etc/sudoers; then echo "nopasswd=$($SUDO grep -rhE '^[^#]*NOPASSWD' /etc/sudoers /etc/sudoers.d 2>/dev/null | wc -l | tr -d ' ')"; else echo nopasswd=?; fi
if [ -f /var/run/reboot-required ]; then echo reboot=yes; else echo reboot=no; fi`

// parseAudit reads the key=value lines of auditScript.
func parseAudit(out string) ServerAudit {
	a := ServerAudit{Firewall: "none", AutoUpd: "none", UID0: []string{}, EmptyPW: []string{}, NoPasswd: -1}
	for _, ln := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(ln), "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "fw":
			a.Firewall = v
		case "auto":
			a.AutoUpd = v
		case "uid0":
			a.UID0 = strings.Fields(v)
		case "emptypw":
			if v != "?" {
				a.PWKnown = true
				a.EmptyPW = strings.Fields(v)
			}
		case "nopasswd":
			if n, err := strconv.Atoi(v); err == nil {
				a.NoPasswd = n
			}
		case "reboot":
			a.Reboot = v == "yes"
		}
	}
	return a
}

// Audit reads the settings of the server that the Security check looks at.
func (s *ServerService) Audit(sid string) (ServerAudit, error) {
	out, err := s.run(sid, asRoot+auditScript)
	if err != nil && out == "" {
		return ServerAudit{}, err
	}
	return parseAudit(out), nil
}

// parseLogins reads `last -n 20 -i -w -F`: user, terminal, address, the start (5 words), then how it ended.
func parseLogins(out string) []ServerLogin {
	res := []ServerLogin{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 8 || f[0] == "reboot" || f[0] == "shutdown" || f[0] == "wtmp" || f[0] == "btmp" {
			continue
		}
		res = append(res, ServerLogin{User: f[0], TTY: f[1], From: f[2], When: strings.Join(f[3:8], " "), Status: strings.Join(f[8:], " ")})
	}
	return res
}

// Logins lists the last logins that worked.
func (s *ServerService) Logins(sid string) ([]ServerLogin, error) {
	out, err := s.run(sid, asRoot+`$SUDO last -n 20 -i -w -F 2>/dev/null || last -n 20 -i -w -F 2>/dev/null; true`)
	if err != nil && out == "" {
		return nil, err
	}
	return parseLogins(out), nil
}
