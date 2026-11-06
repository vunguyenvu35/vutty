package usecase

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// The Security section can also ban an address by hand and change two sshd settings. sshd is never edited in place: the settings go
// in a drop-in file of their own, the whole configuration is tested with `sshd -t`, the value that sshd really ends up with is read
// back with `sshd -T`, and the old file is put back when any of that fails. Settings that could lock the login out are refused.

const sshdDropIn = "/etc/ssh/sshd_config.d/00-vutty.conf"

// sshdSettings maps the name on the screen to the sshd keyword and the values it may take.
var sshdSettings = map[string]struct {
	key    string
	values map[string]bool
}{
	"root_login":    {"PermitRootLogin", map[string]bool{"no": true, "prohibit-password": true, "yes": true}},
	"password_auth": {"PasswordAuthentication", map[string]bool{"no": true, "yes": true}},
}

// mergeSSHDropIn sets one keyword in the content of the drop-in, keeping the others.
func mergeSSHDropIn(content, key, value string) string {
	var out []string
	for _, ln := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		f := strings.Fields(ln)
		if len(f) == 0 || strings.EqualFold(f[0], key) {
			continue
		}
		out = append(out, ln)
	}
	out = append(out, key+" "+value)
	return strings.Join(out, "\n") + "\n"
}

// Ban bans an address on a fail2ban jail. The address of this very connection is refused.
func (s *ServerService) Ban(sid, jail, ip string) error {
	if !nameRe.MatchString(jail) {
		return errors.New("that is not a jail name")
	}
	addr := net.ParseIP(strings.TrimSpace(ip))
	if addr == nil {
		return errors.New("that is not an IP address")
	}
	if addr.IsLoopback() || addr.IsUnspecified() {
		return errors.New("that address cannot be banned")
	}
	mine, _ := s.run(sid, `echo "${SSH_CONNECTION%% *}"`)
	if m := net.ParseIP(strings.TrimSpace(mine)); m != nil && m.Equal(addr) {
		return errors.New("that is the address of this connection: banning it would cut you off")
	}
	_, err := s.run(sid, asRoot+fmt.Sprintf("$SUDO fail2ban-client set %s banip %s", jail, addr.String()))
	s.audit(sid, fmt.Sprintf("banned %s on %s", addr, jail), err)
	return err
}

// SSHSet changes PermitRootLogin ("root_login") or PasswordAuthentication ("password_auth") of sshd.
func (s *ServerService) SSHSet(sid, setting, value string) error {
	def, ok := sshdSettings[setting]
	if !ok || !def.values[value] {
		return errors.New("that sshd setting cannot be changed here")
	}
	cur, err := s.run(sid, asRoot+`$SUDO cat `+sshdDropIn+` 2>/dev/null; true`)
	if err != nil {
		return err
	}
	next := mergeSSHDropIn(cur, def.key, value)
	script := asRoot + fmt.Sprintf(`F=%s; KEY=%s; VAL=%s
[ -d /etc/ssh/sshd_config.d ] && grep -Eq '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config.d/' /etc/ssh/sshd_config 2>/dev/null || { echo "this sshd does not read /etc/ssh/sshd_config.d: edit sshd_config in the terminal" >&2; exit 1; }
`, sshdDropIn, def.key, value)
	switch {
	case setting == "root_login" && value != "yes":
		script += `[ "$(id -u)" = 0 ] && { echo "you are logged in as root: turning root login off would lock you out. Log in as another user first." >&2; exit 1; }
`
	case setting == "password_auth" && value == "no":
		script += `H=$(getent passwd "$(id -un)" | cut -d: -f6)
$SUDO test -s "$H/.ssh/authorized_keys" || { echo "this login has no SSH key on the server: turning password login off would lock you out. Add your key first (Users)." >&2; exit 1; }
`
	}
	script += `T=$(mktemp); cat > "$T"
if [ -f "$F" ]; then B=$(mktemp); $SUDO cp -p "$F" "$B"; else B=; fi
back() { if [ -n "$B" ]; then $SUDO cp -p "$B" "$F"; else $SUDO rm -f "$F"; fi; rm -f "$T" "$B"; }
$SUDO install -m 644 -o root -g root "$T" "$F" || { back; exit 1; }
OUT=$($SUDO sshd -t 2>&1) || { back; echo "$OUT" >&2; exit 1; }
EFF=$($SUDO sshd -T 2>/dev/null | grep -i "^$KEY " | head -1 | cut -d' ' -f2)
[ "$EFF" = "$VAL" ] || [ -z "$EFF" ] || { back; echo "another file sets $KEY to $EFF and wins over this one: edit it in the terminal" >&2; exit 1; }
rm -f "$T" "$B"
$SUDO systemctl reload ssh 2>/dev/null || $SUDO systemctl reload sshd 2>&1`
	x, err := s.get(sid)
	if err != nil {
		return err
	}
	if x.client == nil {
		return errors.New("this is a cluster reached with a kubeconfig: there is no server to run that on")
	}
	_, err = runScriptStdin(x.client, script, strings.NewReader(next), serverTimeout)
	s.audit(sid, fmt.Sprintf("set %s %s in sshd", def.key, value), err)
	return err
}
