package usecase

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// The Users section can also add and delete an account and edit its authorized_keys. Names and shells are checked against a strict
// pattern before they reach a script, and a public key goes in on the standard input, never on the command line.

// UserKey is one line of an authorized_keys file.
type UserKey struct {
	Type        string `json:"type"`        // ssh-ed25519, ssh-rsa ...
	Fingerprint string `json:"fingerprint"` // SHA256:...
	Comment     string `json:"comment"`
	Options     bool   `json:"options"` // the line starts with options (from=, command= ...)
}

var (
	userNameRe  = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	userShellRe = regexp.MustCompile(`^/[A-Za-z0-9/_.-]{1,60}$`)
	fingerRe    = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)
)

// parseAuthorizedKey reads one public key line. A private key is told apart on purpose: pasting it by mistake is the usual error.
func parseAuthorizedKey(line string) (UserKey, error) {
	line = strings.TrimSpace(line)
	switch {
	case line == "":
		return UserKey{}, errors.New("paste a public key")
	case strings.Contains(line, "PRIVATE KEY"):
		return UserKey{}, errors.New("that is a private key: paste the public key (the .pub file)")
	case strings.ContainsAny(line, "\r\n"):
		return UserKey{}, errors.New("a public key is one line")
	}
	pub, comment, opts, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return UserKey{}, errors.New("that is not a public key")
	}
	return UserKey{Type: pub.Type(), Fingerprint: ssh.FingerprintSHA256(pub), Comment: comment, Options: len(opts) > 0}, nil
}

// parseUserKeys lists the keys of an authorized_keys file; comments, blank and unreadable lines are left out.
func parseUserKeys(content string) []UserKey {
	keys := []UserKey{}
	for _, ln := range strings.Split(content, "\n") {
		if t := strings.TrimSpace(ln); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if k, err := parseAuthorizedKey(ln); err == nil {
			keys = append(keys, k)
		}
	}
	return keys
}

// addKeyTo appends a key unless the file has it already (the same key under another comment is the same key).
func addKeyTo(content, line string) (string, error) {
	k, err := parseAuthorizedKey(line)
	if err != nil {
		return "", err
	}
	for _, have := range parseUserKeys(content) {
		if have.Fingerprint == k.Fingerprint {
			return "", errors.New("that key is already there")
		}
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + strings.TrimSpace(line) + "\n", nil
}

// removeKeyFrom drops the lines of a key; every other line (comments included) stays as it was.
func removeKeyFrom(content, fingerprint string) (string, bool) {
	var keep []string
	found := false
	for _, ln := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		if k, err := parseAuthorizedKey(ln); err == nil && k.Fingerprint == fingerprint && !strings.HasPrefix(strings.TrimSpace(ln), "#") {
			found = true
			continue
		}
		keep = append(keep, ln)
	}
	if len(keep) == 0 {
		return "", found
	}
	return strings.Join(keep, "\n") + "\n", found
}

// keyFile puts the home of the user and the path of authorized_keys in $H and $F, from the account database.
func keyFile(name string) string {
	return fmt.Sprintf(`U=%s; H=$(getent passwd "$U" | cut -d: -f6); [ -n "$H" ] || { echo "there is no user $U" >&2; exit 1; }; F="$H/.ssh/authorized_keys"; `, name)
}

func (s *ServerService) readKeyFile(sid, name string) (string, error) {
	if !userNameRe.MatchString(name) {
		return "", errors.New("that is not a user name")
	}
	return s.run(sid, asRoot+keyFile(name)+`$SUDO cat "$F" 2>/dev/null || true`)
}

// writeKeyFile replaces authorized_keys with content (a copy of the old file is kept next to it), with the owner and modes sshd wants.
func (s *ServerService) writeKeyFile(sid, name, content string) error {
	script := asRoot + keyFile(name) + `G=$(id -gn "$U")
[ -d "$H/.ssh" ] || $SUDO install -d -m 700 -o "$U" -g "$G" "$H/.ssh"
[ -f "$F" ] && $SUDO cp -p "$F" "$F.vutty-bak"
$SUDO tee "$F" >/dev/null && $SUDO chown "$U:$G" "$F" && $SUDO chmod 600 "$F"`
	x, err := s.get(sid)
	if err != nil {
		return err
	}
	if x.client == nil {
		return errors.New("this is a cluster reached with a kubeconfig: there is no server to run that on")
	}
	_, err = runScriptStdin(x.client, script, strings.NewReader(content), serverTimeout)
	return err
}

// UserKeys lists the SSH keys of an account.
func (s *ServerService) UserKeys(sid, name string) ([]UserKey, error) {
	out, err := s.readKeyFile(sid, name)
	if err != nil {
		return nil, err
	}
	return parseUserKeys(out), nil
}

// AddUserKey adds a public key to the authorized_keys of an account.
func (s *ServerService) AddUserKey(sid, name, key string) error {
	cur, err := s.readKeyFile(sid, name)
	if err != nil {
		return err
	}
	next, err := addKeyTo(cur, key)
	if err != nil {
		return err
	}
	err = s.writeKeyFile(sid, name, next)
	s.audit(sid, "add an SSH key to "+name, err)
	return err
}

// RemoveUserKey removes the key with this fingerprint from the authorized_keys of an account.
func (s *ServerService) RemoveUserKey(sid, name, fingerprint string) error {
	if !fingerRe.MatchString(fingerprint) {
		return errors.New("that is not a key fingerprint")
	}
	cur, err := s.readKeyFile(sid, name)
	if err != nil {
		return err
	}
	next, found := removeKeyFrom(cur, fingerprint)
	if !found {
		return errors.New("that key is not in the file any more")
	}
	err = s.writeKeyFile(sid, name, next)
	s.audit(sid, "remove an SSH key of "+name, err)
	return err
}

// AddUser creates an account with a home folder and no password (a login by key only), may put it in the sudo group, and may
// install a first public key.
func (s *ServerService) AddUser(sid, name, shell string, sudo bool, key string) error {
	if !userNameRe.MatchString(name) {
		return errors.New("a user name is lower case letters, digits, - and _, and starts with a letter")
	}
	if !userShellRe.MatchString(shell) {
		return errors.New("that is not a shell path")
	}
	key = strings.TrimSpace(key)
	var keyFileText string
	if key != "" {
		var err error
		if keyFileText, err = addKeyTo("", key); err != nil {
			return err
		}
	}
	group := ""
	if sudo {
		group = `G=$(getent group sudo wheel admin | head -n 1 | cut -d: -f1); [ -n "$G" ] || { echo "this server has no sudo, wheel or admin group" >&2; exit 1; }`
	}
	script := asRoot + fmt.Sprintf(`U=%s; SH=%s
id "$U" >/dev/null 2>&1 && { echo "the user $U exists already" >&2; exit 1; }
grep -qx "$SH" /etc/shells || { echo "$SH is not in /etc/shells" >&2; exit 1; }
command -v useradd >/dev/null 2>&1 || { echo "useradd is not on this server" >&2; exit 1; }
%s
$SUDO useradd -m -s "$SH" "$U" || exit 1
if [ -n "$G" ]; then $SUDO usermod -aG "$G" "$U" || exit 1; fi
echo ok`, name, shell, group)
	if _, err := s.runTimeout(sid, script, serverTimeout); err != nil {
		s.audit(sid, "add the user "+name, err)
		return err
	}
	var err error
	if keyFileText != "" {
		err = s.writeKeyFile(sid, name, keyFileText)
	}
	what := "add the user " + name
	if sudo {
		what += " (sudo)"
	}
	s.audit(sid, what, err)
	return err
}

// DeleteUser deletes an account, and its home folder when asked. It will not delete root, a system account, the account of this
// connection, or an account that has a login open.
func (s *ServerService) DeleteUser(sid, name string, removeHome bool) error {
	if !userNameRe.MatchString(name) {
		return errors.New("that is not a user name")
	}
	flag := ""
	if removeHome {
		flag = "-r "
	}
	script := asRoot + fmt.Sprintf(`U=%s
ID=$(id -u "$U" 2>/dev/null) || { echo "there is no user $U" >&2; exit 1; }
[ "$ID" -ge 1000 ] || { echo "$U is a system account: it is not deleted here" >&2; exit 1; }
[ "$(id -un)" != "$U" ] || { echo "this connection is logged in as $U" >&2; exit 1; }
who | awk '{print $1}' | grep -qx "$U" && { echo "$U is logged in now" >&2; exit 1; }
if command -v userdel >/dev/null 2>&1; then $SUDO userdel %s"$U"; else echo "userdel is not on this server" >&2; exit 1; fi`, name, flag)
	_, err := s.runTimeout(sid, script, 30*time.Second)
	what := "delete the user " + name
	if removeHome {
		what += " and its home folder"
	}
	s.audit(sid, what, err)
	return err
}

// Reboot restarts the server. The command is started in the background and returns at once: the connection goes down with the
// machine, and a command that waited for it would report that as an error.
func (s *ServerService) Reboot(sid string) error {
	_, err := s.run(sid, asRoot+`( $SUDO sh -c 'sleep 2; systemctl reboot || reboot' >/dev/null 2>&1 & ); echo ok`)
	s.audit(sid, "reboot the server", err)
	return err
}
