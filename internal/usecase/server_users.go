package usecase

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// The Users section: who can log in to the server, who may use sudo, how many SSH keys each one has, and who is logged in now.
// It only reads. Fixed commands, nothing from the screen reaches the shell.

// ServerUser is an account that can log in (a real shell), or root.
type ServerUser struct {
	Name  string `json:"name"`
	UID   int    `json:"uid"`
	Home  string `json:"home"`
	Shell string `json:"shell"`
	Sudo  bool   `json:"sudo"`  // root, or in the group sudo, wheel or admin
	Keys  int    `json:"keys"`  // lines of authorized_keys; -1 when this login may not read the file
	Login string `json:"login"` // the last login, as `lastlog` says; "" when never or unknown
}

// ServerSession is a login that is open now.
type ServerSession struct {
	User  string `json:"user"`
	TTY   string `json:"tty"`
	Since string `json:"since"`
	From  string `json:"from"`
}

// ServerUsers is the Users section.
type ServerUsers struct {
	Users    []ServerUser    `json:"users"`
	Sessions []ServerSession `json:"sessions"`
}

const usersScript = `echo '##passwd'; getent passwd
echo '##groups'; getent group sudo wheel admin
echo '##keys'; getent passwd | awk -F: '$7 !~ /(nologin|false|sync|shutdown|halt)$/ {print $1" "$6}' | while read u h; do
  if [ -r "$h/.ssh/authorized_keys" ]; then n=$(grep -cE '^[[:space:]]*(ssh-|ecdsa-|sk-)' "$h/.ssh/authorized_keys" 2>/dev/null)
  elif $SUDO test -r "$h/.ssh/authorized_keys" 2>/dev/null; then n=$($SUDO grep -cE '^[[:space:]]*(ssh-|ecdsa-|sk-)' "$h/.ssh/authorized_keys" 2>/dev/null)
  elif [ -e "$h/.ssh/authorized_keys" ] || $SUDO test -e "$h/.ssh/authorized_keys" 2>/dev/null; then n=-1
  else n=0; fi
  echo "$u ${n:-0}"
done
echo '##who'; who 2>/dev/null
echo '##last'; lastlog 2>/dev/null | tail -n +2`

func parseUsers(out string) ServerUsers {
	res := ServerUsers{Users: []ServerUser{}, Sessions: []ServerSession{}}
	sudoers := map[string]bool{}
	for _, ln := range strings.Split(section(out, "groups"), "\n") {
		f := strings.Split(strings.TrimSpace(ln), ":") // name:x:gid:member,member
		if len(f) == 4 {
			for _, m := range strings.Split(f[3], ",") {
				if m = strings.TrimSpace(m); m != "" {
					sudoers[m] = true
				}
			}
		}
	}
	keys := map[string]int{}
	for _, ln := range strings.Split(section(out, "keys"), "\n") {
		f := strings.Fields(ln)
		if len(f) == 2 {
			keys[f[0]], _ = strconv.Atoi(f[1])
		}
	}
	last := map[string]string{}
	for _, ln := range strings.Split(section(out, "last"), "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 || strings.Contains(ln, "**Never logged in**") {
			continue
		}
		last[f[0]] = strings.Join(f[len(f)-5:], " ") // the date and time are the last five words of a lastlog line
	}
	for _, ln := range strings.Split(section(out, "passwd"), "\n") {
		f := strings.Split(strings.TrimSpace(ln), ":")
		if len(f) < 7 {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		shell := f[6]
		if err != nil || (uid != 0 && uid < 1000) || uid == 65534 || strings.HasSuffix(shell, "nologin") || strings.HasSuffix(shell, "false") {
			continue
		}
		k, ok := keys[f[0]]
		if !ok {
			k = 0
		}
		res.Users = append(res.Users, ServerUser{Name: f[0], UID: uid, Home: f[5], Shell: shell, Sudo: uid == 0 || sudoers[f[0]], Keys: k, Login: last[f[0]]})
	}
	sort.SliceStable(res.Users, func(i, j int) bool { return res.Users[i].UID < res.Users[j].UID })
	for _, ln := range strings.Split(section(out, "who"), "\n") {
		f := strings.Fields(ln) // user tty 2026-01-02 10:11 (1.2.3.4)
		if len(f) < 4 {
			continue
		}
		s := ServerSession{User: f[0], TTY: f[1], Since: f[2] + " " + f[3]}
		if i := strings.Index(ln, "("); i >= 0 {
			s.From = strings.Trim(ln[i:], "() \t")
		}
		res.Sessions = append(res.Sessions, s)
	}
	return res
}

// Users lists the accounts that can log in and the logins open now.
func (s *ServerService) Users(sid string) (ServerUsers, error) {
	out, err := s.runTimeout(sid, asRoot+usersScript, 30*time.Second)
	if err != nil {
		return ServerUsers{}, err
	}
	return parseUsers(out), nil
}
