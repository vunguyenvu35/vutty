package usecase

import (
	"strconv"
	"strings"
)

// Installing the waiting package updates. The command runs in the background on the server and writes to a log file there, so that it
// goes on when the panel is closed (an upgrade that is cut half way leaves a broken system) and the screen can read how far it is.
// The command is fixed: nothing from the screen reaches it.

// The files live in a folder of the login (mode 700), never in /var/tmp: a file there that root runs or reads could be put there by any
// other user of the server.
const upgradeDir = `D="$HOME/.cache/vutty"; mkdir -p -m 700 "$D"; LOG="$D/upgrade.log"; RC="$D/upgrade.rc"; PID="$D/upgrade.pid"`

// ServerUpgrade is how far an upgrade started from the panel is.
type ServerUpgrade struct {
	Running bool   `json:"running"`
	Done    bool   `json:"done"`    // it has ended (the exit code is known)
	OK      bool   `json:"ok"`      // it ended with exit code 0
	Code    int    `json:"code"`    // the exit code, when Done
	Log     string `json:"log"`     // the end of what it printed
	Started bool   `json:"started"` // there is a log of an upgrade at all
}

// upgradeCommand is the command of the package manager of the server, run as root.
const upgradeCommand = `if command -v apt-get >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -q && apt-get -y -q -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold upgrade
elif command -v dnf >/dev/null 2>&1; then dnf -y upgrade
elif command -v yum >/dev/null 2>&1; then yum -y update
else echo "No supported package manager was found." >&2; exit 1; fi`

// upgradeStartScript starts the command in the background, unless one is running already. The command goes to sh as an argument: no
// file is written that root would run.
func upgradeStartScript() string {
	return asRoot + upgradeDir + `
if [ -f "$PID" ] && kill -0 "$(cat "$PID" 2>/dev/null)" 2>/dev/null; then echo "an upgrade is running already" >&2; exit 1; fi
rm -f "$RC"; : > "$LOG"; chmod 600 "$LOG"
CMD=$(cat <<'VUTTY_EOF'
` + upgradeCommand + `
VUTTY_EOF
)
if [ "$(id -u)" = 0 ]; then set -- sh -c "$CMD"; else set -- sudo -n sh -c "$CMD"; fi
nohup sh -c '"$@"; echo $? > "$0"' "$RC" "$@" >> "$LOG" 2>&1 < /dev/null &
echo $! > "$PID"
echo started`
}

var upgradeStatusScript = upgradeDir + `
[ -f "$LOG" ] || { echo '##none'; exit 0; }
if [ -f "$PID" ] && kill -0 "$(cat "$PID" 2>/dev/null)" 2>/dev/null; then echo '##running'; fi
if [ -f "$RC" ]; then echo "##rc $(cat "$RC")"; fi
echo '##log'; tail -n 200 "$LOG" | tail -c 16000`

// parseUpgrade reads the output of upgradeStatusScript.
func parseUpgrade(out string) ServerUpgrade {
	var u ServerUpgrade
	if strings.Contains(out, "##none") {
		return u
	}
	u.Started = true
	var log []string
	inLog := false
	for _, ln := range strings.Split(out, "\n") {
		switch {
		case inLog:
			log = append(log, ln)
		case ln == "##log":
			inLog = true
		case ln == "##running":
			u.Running = true
		case strings.HasPrefix(ln, "##rc "):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(ln, "##rc "))); err == nil {
				u.Done, u.Code, u.OK = true, n, n == 0
			}
		}
	}
	u.Log = strings.TrimRight(strings.Join(log, "\n"), "\n")
	if u.Running {
		u.Done = false
	}
	return u
}

// UpdatesInstall starts installing every waiting update on the server (apt, dnf or yum), in the background.
func (s *ServerService) UpdatesInstall(sid string) error {
	_, err := s.run(sid, upgradeStartScript())
	s.audit(sid, "install the package updates", err)
	return err
}

// UpdatesInstallStatus says how far the upgrade started from the panel is (Started is false when none was started).
func (s *ServerService) UpdatesInstallStatus(sid string) (ServerUpgrade, error) {
	out, err := s.run(sid, upgradeStatusScript)
	if err != nil && out == "" {
		return ServerUpgrade{}, err
	}
	return parseUpgrade(out), nil
}
