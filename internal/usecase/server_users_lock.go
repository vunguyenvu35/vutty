package usecase

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Lock an account and give or take away sudo. Locking uses usermod -L (no password login) together with an account that has ended
// (-e 1), because a locked password alone does not stop a login with an SSH key. Neither is done to root, to a system account or to
// the account of this connection, and sudo is never taken from the last account that has it.

// SetUserLocked locks or unlocks an account.
func (s *ServerService) SetUserLocked(sid, name string, locked bool) error {
	if !userNameRe.MatchString(name) {
		return errors.New("that is not a user name")
	}
	cmd := `$SUDO usermod -U -e "" "$U"`
	what := "unlock the user "
	if locked {
		cmd = `who | awk '{print $1}' | grep -qx "$U" && { echo "$U is logged in now: it would stay logged in after the lock" >&2; exit 1; }
$SUDO usermod -L -e 1 "$U"`
		what = "lock the user "
	}
	script := asRoot + fmt.Sprintf(`U=%s
ID=$(id -u "$U" 2>/dev/null) || { echo "there is no user $U" >&2; exit 1; }
[ "$ID" -ge 1000 ] || { echo "$U is root or a system account: it is not locked here" >&2; exit 1; }
[ "$(id -un)" != "$U" ] || { echo "this connection is logged in as $U" >&2; exit 1; }
command -v usermod >/dev/null 2>&1 || { echo "usermod is not on this server" >&2; exit 1; }
%s`, name, cmd)
	_, err := s.runTimeout(sid, script, 30*time.Second)
	s.audit(sid, what+name, err)
	return err
}

// SetUserSudo adds an account to the sudo group of the server (sudo, wheel or admin), or takes it out of all of them.
func (s *ServerService) SetUserSudo(sid, name string, sudo bool) error {
	if !userNameRe.MatchString(name) {
		return errors.New("that is not a user name")
	}
	body := `G=$(getent group sudo wheel admin | head -n 1 | cut -d: -f1); [ -n "$G" ] || { echo "this server has no sudo, wheel or admin group" >&2; exit 1; }
$SUDO usermod -aG "$G" "$U"`
	what := "give sudo to "
	if !sudo {
		what = "take sudo from "
		body = `OTHERS=$(getent group sudo wheel admin | cut -d: -f4 | tr ',' '\n' | grep -v '^$' | sort -u | grep -vx "$U" | wc -l | tr -d ' ')
[ "$OTHERS" -gt 0 ] || { echo "$U is the last account with sudo: another one must have it first" >&2; exit 1; }
for G in sudo wheel admin; do
  if getent group "$G" >/dev/null 2>&1 && id -nG "$U" | tr ' ' '\n' | grep -qx "$G"; then $SUDO gpasswd -d "$U" "$G" >/dev/null || exit 1; fi
done`
	}
	script := asRoot + fmt.Sprintf(`U=%s
ID=$(id -u "$U" 2>/dev/null) || { echo "there is no user $U" >&2; exit 1; }
[ "$ID" -ge 1000 ] || { echo "$U is root or a system account" >&2; exit 1; }
[ "$(id -un)" != "$U" ] || [ "%t" = true ] || { echo "this connection is logged in as $U: ask another account to take its sudo away" >&2; exit 1; }
%s`, name, sudo, body)
	_, err := s.runTimeout(sid, script, 30*time.Second)
	s.audit(sid, what+name, err)
	return err
}

// parseLocked reads `passwd -S -a`: name, then L or LK for a locked password.
func parseLocked(out string) map[string]bool {
	m := map[string]bool{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) >= 2 && (f[1] == "L" || f[1] == "LK") {
			m[f[0]] = true
		}
	}
	return m
}
