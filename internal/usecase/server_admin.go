package usecase

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// The changes of the Users, Ports and firewall, Certificates and Schedule sections: SSH keys, accounts, ufw rules, certificate renewal
// and the crontab. Fixed commands; every name, port and key is checked here before it reaches the shell, a key or a cron line travels
// on the standard input, every change is recorded (audit) and the screen asks first. What would cut the panel off from the server
// (the account it logs in as, the SSH port) is refused here, not only on the screen.

var (
	userRe     = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	fpRe       = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)
	pubKeyRe   = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com) [A-Za-z0-9+/]+={0,3}( [^\r\n]{0,128})?$`)
	cronTimeRe = regexp.MustCompile(`^(@(reboot|hourly|daily|weekly|monthly|yearly|annually)|[0-9*/,\-]+ [0-9*/,\-]+ [0-9*/,\-]+ [0-9*/,\-A-Za-z]+ [0-9*/,\-A-Za-z]+)$`)
)

// userHome is the lines every key script starts with: the home of the account, and its authorized_keys.
func userHome(u string) string {
	return fmt.Sprintf(`u=%s; h=$(getent passwd "$u" | cut -d: -f6); [ -n "$h" ] || { echo "there is no account $u" >&2; exit 1; }; f="$h/.ssh/authorized_keys"
`, u)
}

// ServerKey is a public key of an account.
type ServerKey struct {
	Fingerprint string `json:"fingerprint"`
	Type        string `json:"type"`
	Comment     string `json:"comment"`
}

func parseKeys(out string) []ServerKey {
	res := []ServerKey{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln) // 256 SHA256:abc… comment words (ED25519)
		if len(f) < 3 || !fpRe.MatchString(f[1]) {
			continue
		}
		k := ServerKey{Fingerprint: f[1], Type: strings.Trim(f[len(f)-1], "()")}
		if len(f) > 3 {
			k.Comment = strings.Join(f[2:len(f)-1], " ")
		}
		res = append(res, k)
	}
	return res
}

// SSHKeys lists the keys in the authorized_keys of an account.
func (s *ServerService) SSHKeys(sid, user string) ([]ServerKey, error) {
	if !userRe.MatchString(user) {
		return nil, errors.New("that is not an account name")
	}
	out, err := s.run(sid, asRoot+userHome(user)+`$SUDO cat "$f" 2>/dev/null | while IFS= read -r line; do
  case "$line" in ''|'#'*) continue;; esac
  echo "$line" | ssh-keygen -lf /dev/stdin 2>/dev/null
done; true`)
	if err != nil {
		return nil, err
	}
	return parseKeys(out), nil
}

// AddSSHKey adds a public key to the authorized_keys of an account (nothing happens when it is there already). The key goes in on the
// standard input.
func (s *ServerService) AddSSHKey(sid, user, key string) error {
	key = strings.TrimSpace(key)
	if !userRe.MatchString(user) {
		return errors.New("that is not an account name")
	}
	if !pubKeyRe.MatchString(key) {
		return errors.New("that is not a public key: paste one line, like ssh-ed25519 AAAA… name")
	}
	x, err := s.get(sid)
	if err != nil {
		return err
	}
	if x.client == nil {
		return errors.New("there is no server here")
	}
	_, err = runScriptStdin(x.client, asRoot+userHome(user)+`key=$(cat)
fp=$(echo "$key" | ssh-keygen -lf /dev/stdin 2>/dev/null | awk '{print $2}'); [ -n "$fp" ] || { echo "that is not a valid public key" >&2; exit 1; }
if $SUDO test -f "$f" && $SUDO cat "$f" | ssh-keygen -lf /dev/stdin 2>/dev/null | grep -qF "$fp"; then echo "that key is already there" >&2; exit 1; fi
$SUDO install -d -m 700 -o "$u" -g "$(id -gn "$u")" "$h/.ssh" || exit 1
if $SUDO test -s "$f" && [ -n "$($SUDO tail -c1 "$f")" ]; then echo | $SUDO tee -a "$f" >/dev/null; fi
printf '%s\n' "$key" | $SUDO tee -a "$f" >/dev/null
$SUDO chown "$u" "$f"; $SUDO chmod 600 "$f"`, strings.NewReader(key), serverTimeout)
	s.audit(sid, "add an SSH key to "+user, err)
	return err
}

// RemoveSSHKey removes the key with this fingerprint from the authorized_keys of an account.
func (s *ServerService) RemoveSSHKey(sid, user, fingerprint string) error {
	if !userRe.MatchString(user) {
		return errors.New("that is not an account name")
	}
	if !fpRe.MatchString(fingerprint) {
		return errors.New("that is not a key fingerprint")
	}
	_, err := s.run(sid, asRoot+userHome(user)+`fp=`+fingerprint+`
$SUDO test -f "$f" || { echo "there is no authorized_keys for $u" >&2; exit 1; }
tmp=$(mktemp) || exit 1; trap 'rm -f "$tmp"' EXIT
$SUDO cat "$f" | while IFS= read -r line; do
  case "$line" in ''|'#'*) echo "$line"; continue;; esac
  [ "$(echo "$line" | ssh-keygen -lf /dev/stdin 2>/dev/null | awk '{print $2}')" = "$fp" ] && continue
  echo "$line"
done > "$tmp"
$SUDO tee "$f" < "$tmp" >/dev/null`)
	s.audit(sid, "remove an SSH key from "+user, err)
	return err
}

// AddUser creates an account with a home folder and a bash shell, and no password (it can only be reached with a key added after).
func (s *ServerService) AddUser(sid, name string, sudo bool) error {
	if !userRe.MatchString(name) {
		return errors.New("an account name has lowercase letters, digits, - and _, and starts with a letter")
	}
	cmd := "$SUDO useradd -m -s /bin/bash " + name
	if sudo {
		cmd += `; g=sudo; getent group sudo >/dev/null || g=wheel; $SUDO usermod -aG "$g" ` + name
	}
	_, err := s.run(sid, asRoot+cmd)
	s.audit(sid, "add the account "+name, err)
	return err
}

// SetSudo gives an account the right to use sudo (the group sudo, or wheel), or takes it away. The account the panel logs in as keeps it.
func (s *ServerService) SetSudo(sid, name string, on bool) error {
	if !userRe.MatchString(name) {
		return errors.New("that is not an account name")
	}
	x, err := s.get(sid)
	if err != nil {
		return err
	}
	if !on && name == x.login {
		return errors.New("this is the account the panel is logged in as: it keeps sudo")
	}
	if name == "root" {
		return errors.New("root always has it")
	}
	cmd := `g=sudo; getent group sudo >/dev/null || g=wheel; $SUDO usermod -aG "$g" ` + name
	if !on {
		cmd = `for g in sudo wheel admin; do getent group $g >/dev/null && $SUDO gpasswd -d ` + name + ` $g >/dev/null 2>&1; done; true`
	}
	_, err = s.run(sid, asRoot+cmd)
	s.audit(sid, fmt.Sprintf("set sudo of %s to %v", name, on), err)
	return err
}

// DeleteUser removes an account (and its home folder when removeHome). Root, the account the panel logs in as and system accounts stay.
func (s *ServerService) DeleteUser(sid, name string, removeHome bool) error {
	if !userRe.MatchString(name) {
		return errors.New("that is not an account name")
	}
	x, err := s.get(sid)
	if err != nil {
		return err
	}
	if name == "root" || name == x.login {
		return errors.New("this account cannot be deleted from here: it is root, or the one the panel is logged in as")
	}
	flag := ""
	if removeHome {
		flag = "-r "
	}
	_, err = s.run(sid, asRoot+fmt.Sprintf(`id=$(id -u %[1]s 2>/dev/null) || { echo "there is no account %[1]s" >&2; exit 1; }
[ "$id" -ge 1000 ] || { echo "that is a system account" >&2; exit 1; }
$SUDO pkill -KILL -u %[1]s 2>/dev/null; $SUDO userdel %[2]s%[1]s`, name, flag))
	s.audit(sid, "delete the account "+name, err)
	return err
}

// FirewallAllow opens a port in ufw. It does not turn the firewall on.
func (s *ServerService) FirewallAllow(sid string, port int, proto string) error {
	if port < 1 || port > 65535 || (proto != "tcp" && proto != "udp") {
		return errors.New("a port is 1 to 65535, and the protocol tcp or udp")
	}
	_, err := s.run(sid, asRoot+fmt.Sprintf("$SUDO ufw allow %d/%s", port, proto))
	s.audit(sid, fmt.Sprintf("open port %d/%s", port, proto), err)
	return err
}

// FirewallDelete closes a port that ufw has an allow rule for. The SSH port of this connection is refused.
func (s *ServerService) FirewallDelete(sid string, port int, proto string) error {
	if port < 1 || port > 65535 || (proto != "tcp" && proto != "udp") {
		return errors.New("a port is 1 to 65535, and the protocol tcp or udp")
	}
	x, err := s.get(sid)
	if err != nil {
		return err
	}
	if port == x.sshPort {
		return errors.New("that is the port the panel is connected through: closing it would cut it off")
	}
	_, err = s.run(sid, asRoot+fmt.Sprintf("$SUDO ufw delete allow %d/%s", port, proto))
	s.audit(sid, fmt.Sprintf("close port %d/%s", port, proto), err)
	return err
}

// CertRenew renews a Let's Encrypt certificate with certbot; dryRun only tests that it could. It returns what certbot said.
func (s *ServerService) CertRenew(sid, name string, dryRun bool) (string, error) {
	if !nameRe.MatchString(name) {
		return "", errors.New("that is not a certificate name")
	}
	flag := ""
	if dryRun {
		flag = " --dry-run"
	}
	out, err := s.runTimeout(sid, asRoot+"$SUDO certbot renew --cert-name "+name+flag+" 2>&1 | tail -n 40", serverLongTimeout)
	if !dryRun {
		s.audit(sid, "renew the certificate "+name, err)
	}
	return strings.TrimSpace(out), err
}

// CronAdd adds a line to the crontab of the account the panel logs in as. The command is the one thing here the person writes: the screen shows it whole
// before it is added, and it travels on the standard input.
func (s *ServerService) CronAdd(sid, when, command string) error {
	when, command = strings.TrimSpace(when), strings.TrimSpace(command)
	if !cronTimeRe.MatchString(when) {
		return errors.New("that is not a cron schedule: five fields, like 0 3 * * *, or @daily")
	}
	if command == "" || len(command) > 500 || strings.ContainsAny(command, "\r\n\x00") {
		return errors.New("the command is one line, up to 500 characters")
	}
	return s.cronEdit(sid, "add a cron job", when+" "+command, false)
}

// CronRemove removes a line (exactly as the Schedule section shows it) from the crontab of the account the panel logs in as.
func (s *ServerService) CronRemove(sid, line string) error {
	if strings.ContainsAny(line, "\r\n\x00") {
		return errors.New("that is not a cron line")
	}
	line = strings.Join(strings.Fields(line), " ") // spaces as one, like the comparison in the script
	if line == "" || len(line) > 700 {
		return errors.New("that is not a cron line")
	}
	return s.cronEdit(sid, "remove a cron job", line, true)
}

func (s *ServerService) cronEdit(sid, what, line string, remove bool) error {
	x, err := s.get(sid)
	if err != nil {
		return err
	}
	if x.client == nil {
		return errors.New("there is no server here")
	}
	script := `line=$(cat); tmp=$(mktemp) || exit 1; trap 'rm -f "$tmp"' EXIT
crontab -l 2>/dev/null > "$tmp"
`
	if remove {
		script += `L="$line" awk 'BEGIN{l=ENVIRON["L"]} {c=$0; gsub(/[ \t]+/," ",c); sub(/^ /,"",c); sub(/ $/,"",c); if (c==l) next} {print}' "$tmp" > "$tmp.n"; if cmp -s "$tmp" "$tmp.n"; then rm -f "$tmp.n"; echo "that line is not in the crontab" >&2; exit 1; fi; mv "$tmp.n" "$tmp"
`
	} else {
		script += `printf '%s\n' "$line" >> "$tmp"
`
	}
	script += `crontab "$tmp"`
	_, err = runScriptStdin(x.client, script, strings.NewReader(line), 20*time.Second)
	s.audit(sid, what, err)
	return err
}
