package usecase

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"vutty/internal/domain"
)

// The tools below add to the Server panel what a person who runs a VPS by hand looks at: certificates, the web server,
// Compose projects, updates, disk space, logins, scheduled jobs, backups, and a look at every saved host. They follow the
// rules of the rest of ServerService: fixed commands, nothing the screen sends is put in a command unless it is checked first
// (nameRe, composeNameRe, ValidServerPath, net.ParseIP), every change asks first (the screen) and is recorded (audit).

const certsScript = `if $SUDO test -d /etc/letsencrypt/live; then echo '@@@found'; fi
$SUDO find /etc/letsencrypt/live -mindepth 2 -maxdepth 2 -name cert.pem 2>/dev/null | sort | while read -r f; do
  echo "@@@cert $(basename "$(dirname "$f")")"
  $SUDO openssl x509 -in "$f" -noout -enddate 2>/dev/null
  $SUDO openssl x509 -in "$f" -noout -ext subjectAltName 2>/dev/null || $SUDO openssl x509 -in "$f" -noout -text 2>/dev/null | grep -A1 'Subject Alternative Name'
done
echo '@@@now'; date +%s
true`

// Certificates lists the Let's Encrypt certificates of the server, the soonest to expire first.
func (s *ServerService) Certificates(sid string) (ServerCerts, error) {
	out, err := s.run(sid, asRoot+certsScript)
	if err != nil {
		return ServerCerts{}, err
	}
	return parseCerts(out), nil
}

const webScript = `if command -v nginx >/dev/null 2>&1; then
  echo '@@@kind'; echo nginx
  echo '@@@version'; nginx -v 2>&1
  echo '@@@running'; systemctl is-active nginx 2>/dev/null || { pgrep -x nginx >/dev/null 2>&1 && echo active; }
  echo '@@@conf'; $SUDO nginx -T 2>/dev/null
elif command -v caddy >/dev/null 2>&1; then
  echo '@@@kind'; echo caddy
  echo '@@@version'; caddy version 2>&1 | head -1
  echo '@@@running'; systemctl is-active caddy 2>/dev/null || { pgrep -x caddy >/dev/null 2>&1 && echo active; }
  echo '@@@conf'; $SUDO cat /etc/caddy/Caddyfile 2>/dev/null
fi
true`

// Web reports the web server (nginx, else Caddy) and the sites in its configuration.
func (s *ServerService) Web(sid string) (ServerWeb, error) {
	out, err := s.run(sid, asRoot+webScript)
	if err != nil {
		return ServerWeb{}, err
	}
	return parseWeb(out), nil
}

// webLogCommand is the fixed command that shows the log of a web server ("" for any other name).
func webLogCommand(name string, lines int, jSince string) string {
	switch name {
	case "nginx":
		return fmt.Sprintf("$SUDO tail -n %d /var/log/nginx/error.log 2>&1", lines)
	case "caddy":
		return fmt.Sprintf("journalctl -u caddy.service -n %d%s --no-pager -o short-iso 2>&1", lines, jSince)
	}
	return ""
}

var webTests = map[string]string{
	"nginx": "$SUDO nginx -t 2>&1",
	"caddy": "$SUDO caddy validate --config /etc/caddy/Caddyfile 2>&1",
}

var webReloads = map[string]string{
	"nginx": "$SUDO systemctl reload nginx 2>&1",
	"caddy": "$SUDO systemctl reload caddy 2>&1 || $SUDO caddy reload --config /etc/caddy/Caddyfile 2>&1",
}

// webScriptFor prints the exit code, then the output; with reload the server is reloaded only when the test passed.
func webScriptFor(kind string, reload bool) (string, error) {
	test, ok := webTests[kind]
	if !ok {
		return "", errors.New("the web server must be nginx or caddy")
	}
	script := "OUT=$(" + test + "); RC=$?\n"
	if reload {
		script += "if [ $RC -eq 0 ]; then R=$(" + webReloads[kind] + "); RC=$?; OUT=\"$OUT\n$R\"; fi\n"
	}
	return asRoot + script + "echo $RC; echo \"$OUT\"", nil
}

// WebTest checks the configuration (nginx -t, caddy validate) and changes nothing. A configuration that fails is no error: OK is false and Output says why.
func (s *ServerService) WebTest(sid, kind string) (ServerWebResult, error) {
	script, err := webScriptFor(kind, false)
	if err != nil {
		return ServerWebResult{}, err
	}
	out, err := s.run(sid, script)
	if err != nil {
		return ServerWebResult{}, err
	}
	return parseTestResult(out), nil
}

// WebReload tests the configuration and reloads the web server only when the test passed.
func (s *ServerService) WebReload(sid, kind string) (ServerWebResult, error) {
	script, err := webScriptFor(kind, true)
	if err != nil {
		return ServerWebResult{}, err
	}
	out, err := s.run(sid, script)
	if err != nil {
		return ServerWebResult{}, err
	}
	res := parseTestResult(out)
	if res.OK {
		s.audit(sid, "reloaded "+kind, nil)
	} else {
		s.audit(sid, "reload "+kind, errors.New(res.Output))
	}
	return res, nil
}

// composeScript runs docker compose commands on a project that is already running: where its files are comes from the labels of
// its first container, so no path from the screen is ever used. Each command is the arguments that follow `docker compose -p <name>`.
// The caller puts asRoot in front.
func composeScript(project string, cmds ...string) string {
	script := fmt.Sprintf(`D=docker; $D ps >/dev/null 2>&1 || D="$SUDO docker"
C=$($D ps -aq --filter label=com.docker.compose.project=%[1]s | head -1)
[ -n "$C" ] || { echo "No container of the Compose project %[1]s was found." >&2; exit 1; }
WD=$($D inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$C")
CF=$($D inspect --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}' "$C")
set --
OLDIFS=$IFS; IFS=,; for f in $CF; do set -- "$@" -f "$f"; done; IFS=$OLDIFS
if [ -n "$WD" ]; then cd "$WD" || { echo "The folder of the project ($WD) is gone." >&2; exit 1; }; fi
`, project)
	for i, c := range cmds {
		if i > 0 {
			script += " && "
		}
		script += fmt.Sprintf(`$D compose "$@" -p %s %s`, project, c)
	}
	return script
}

var stackActions = map[string][]string{
	"start":   {"start"},
	"stop":    {"stop"},
	"restart": {"restart"},
	"update":  {"pull", "up -d"},
}

// StackAction starts, stops or restarts every container of a Compose project, or updates it (pulls the images, then up -d).
func (s *ServerService) StackAction(sid, project, action string) error {
	if !composeNameRe.MatchString(project) {
		return errors.New("that is not a Compose project name")
	}
	cmds, ok := stackActions[action]
	if !ok {
		return errors.New("unknown action")
	}
	limit := serverTimeout
	if action == "update" {
		limit = serverLongTimeout
	}
	_, err := s.runTimeout(sid, asRoot+composeScript(project, cmds...), limit)
	s.audit(sid, fmt.Sprintf("%s Compose project %s", action, project), err)
	return err
}

const updatesScript = `if command -v apt-get >/dev/null 2>&1; then
  echo '@@@mgr'; echo apt
  echo '@@@list'; apt list --upgradable 2>/dev/null
  echo '@@@reboot'; [ -f /var/run/reboot-required ] && echo yes
  echo '@@@stamp'; stat -c %Y /var/lib/apt/periodic/update-success-stamp 2>/dev/null || stat -c %Y /var/lib/apt/lists 2>/dev/null
  echo '@@@now'; date +%s
else
  PM=; command -v dnf >/dev/null 2>&1 && PM=dnf; [ -z "$PM" ] && command -v yum >/dev/null 2>&1 && PM=yum
  if [ -n "$PM" ]; then
    echo '@@@mgr'; echo $PM
    echo '@@@list'; $PM -q check-update 2>/dev/null
    echo '@@@sec'; $PM -q updateinfo list security 2>/dev/null
    echo '@@@reboot'; command -v needs-restarting >/dev/null 2>&1 && { needs-restarting -r >/dev/null 2>&1 || echo yes; }
  fi
fi
true`

// Updates counts the packages that have an update (from the package lists as they are: see ListsAgeSec), the security ones among them, and says whether a reboot is waiting.
func (s *ServerService) Updates(sid string) (ServerUpdates, error) {
	out, err := s.runTimeout(sid, updatesScript, serverLongTimeout)
	if err != nil {
		return ServerUpdates{}, err
	}
	return parseUpdates(out), nil
}

// UpdatesRefresh refreshes the package lists (apt-get update, dnf makecache), so that Updates counts what is there now.
func (s *ServerService) UpdatesRefresh(sid string) error {
	_, err := s.runTimeout(sid, asRoot+`if command -v apt-get >/dev/null 2>&1; then $SUDO apt-get update -q
elif command -v dnf >/dev/null 2>&1; then $SUDO dnf -q makecache
elif command -v yum >/dev/null 2>&1; then $SUDO yum -q makecache
else echo "No supported package manager was found." >&2; exit 1; fi`, serverLongTimeout)
	s.audit(sid, "refreshed the package lists", err)
	return err
}

const cleanupScript = `echo '@@@df'; df -PkT 2>/dev/null
if command -v docker >/dev/null 2>&1; then
  D=docker; docker info >/dev/null 2>&1 || D="$SUDO docker"
  echo '@@@docker'
  echo '@@@dockerdf'; $D system df --format '{{json .}}' 2>/dev/null
  echo '@@@dangling'; $D images -f dangling=true --format '{{.Size}}' 2>/dev/null
fi
if command -v journalctl >/dev/null 2>&1; then echo '@@@journal'; journalctl --disk-usage 2>/dev/null; fi
if command -v apt-get >/dev/null 2>&1; then echo '@@@apt'; echo '@@@aptsize'; du -sk /var/cache/apt 2>/dev/null; fi
true`

// Cleanup shows the file systems and what could be cleaned, with about how much each would free.
func (s *ServerService) Cleanup(sid string) (ServerCleanup, error) {
	out, err := s.runTimeout(sid, asRoot+cleanupScript, serverLongTimeout)
	if err != nil {
		return ServerCleanup{}, err
	}
	return parseCleanup(out), nil
}

// cleanups are the only things CleanupRun does. Volumes are not among them: they may hold data.
var cleanups = map[string]string{
	"docker-dangling": `D=docker; docker info >/dev/null 2>&1 || D="$SUDO docker"; $D image prune -f && $D builder prune -f`,
	"docker-images":   `D=docker; docker info >/dev/null 2>&1 || D="$SUDO docker"; $D image prune -af`,
	"journal":         `$SUDO journalctl --vacuum-size=200M`,
	"apt-cache":       `$SUDO apt-get clean`,
}

// CleanupRun does one cleanup of Cleanup, by its id.
func (s *ServerService) CleanupRun(sid, id string) error {
	cmd, ok := cleanups[id]
	if !ok {
		return errors.New("that cannot be cleaned from here")
	}
	_, err := s.runTimeout(sid, asRoot+cmd, serverLongTimeout)
	s.audit(sid, "cleaned "+id, err)
	return err
}

// DiskUsage lists the folders directly under dir, the biggest first.
func (s *ServerService) DiskUsage(sid, dir string) ([]ServerFolder, error) {
	if !domain.ValidServerPath(dir) {
		return nil, errors.New("that is not a folder path")
	}
	out, err := s.runTimeout(sid, asRoot+fmt.Sprintf(`$SUDO du -xk --max-depth=1 -- "%s" 2>/dev/null; true`, dir), 60*time.Second)
	if err != nil {
		return nil, err
	}
	return parseFolders(out, dir), nil
}

const securityScript = `if command -v fail2ban-client >/dev/null 2>&1; then
  echo '@@@f2binstalled'
  S=$($SUDO fail2ban-client status 2>/dev/null) && {
    echo '@@@f2brunning'
    for j in $(echo "$S" | sed -n 's/.*Jail list:[[:space:]]*//p' | tr ',' ' '); do
      case "$j" in *[!A-Za-z0-9_.@:-]*) continue;; esac
      echo "@@@jail $j"; $SUDO fail2ban-client status "$j" 2>/dev/null
    done
  }
fi
echo '@@@ssh'; $SUDO $(command -v sshd || echo /usr/sbin/sshd) -T 2>/dev/null
K=0; L=$(journalctl -u ssh -u sshd --since '24 hours ago' --no-pager -o cat 2>/dev/null)
if [ -n "$L" ]; then K=1; else
  for f in /var/log/auth.log /var/log/secure; do
    if $SUDO test -r "$f"; then L=$($SUDO tail -n 20000 "$f" 2>/dev/null); K=1; break; fi
  done
fi
echo '@@@known'; echo $K
echo '@@@auth'; echo "$L" | grep -E 'Failed password|Invalid user' | tail -n 5000
true`

// Security reports fail2ban (its jails and the addresses it has banned), the sshd settings that matter, and the failed SSH logins of the last 24 hours.
func (s *ServerService) Security(sid string) (ServerSecurity, error) {
	out, err := s.runTimeout(sid, asRoot+securityScript, 40*time.Second)
	if err != nil {
		return ServerSecurity{}, err
	}
	return parseSecurity(out), nil
}

// Unban lets an address back in on a fail2ban jail.
func (s *ServerService) Unban(sid, jail, ip string) error {
	if !nameRe.MatchString(jail) {
		return errors.New("that is not a jail name")
	}
	addr := net.ParseIP(ip)
	if addr == nil {
		return errors.New("that is not an IP address")
	}
	_, err := s.run(sid, asRoot+fmt.Sprintf("$SUDO fail2ban-client set %s unbanip %s", jail, addr.String()))
	s.audit(sid, fmt.Sprintf("unbanned %s from %s", addr, jail), err)
	return err
}

const scheduleScript = `for t in $(systemctl list-units --type=timer --all --plain --no-legend --no-pager 2>/dev/null | awk '{print $1}'); do
  case "$t" in *[!A-Za-z0-9_.@:-]*) continue;; esac
  echo "@@@timer $t"
  systemctl show "$t" -p Description -p Unit -p NextElapseUSecRealtime -p LastTriggerUSec 2>/dev/null
  U=$(systemctl show "$t" -p Unit 2>/dev/null | cut -d= -f2)
  echo '@@@result'
  case "$U" in ''|*[!A-Za-z0-9_.@:-]*) ;; *) systemctl show "$U" -p Result 2>/dev/null;; esac
done
if [ "$(id -u)" = 0 ]; then
  echo '@@@cron root'; crontab -l 2>/dev/null
else
  echo '@@@cron crontab'; crontab -l 2>/dev/null
  echo '@@@cron root'; $SUDO crontab -u root -l 2>/dev/null
fi
echo '@@@cron /etc/crontab'; cat /etc/crontab 2>/dev/null
for f in /etc/cron.d/*; do [ -f "$f" ] || continue; echo "@@@cron $f"; cat "$f" 2>/dev/null; done
echo '@@@me'; id -un
true`

// Schedule lists the systemd timers (when they ran and will run, how the last run ended) and the cron jobs.
func (s *ServerService) Schedule(sid string) (ServerSchedule, error) {
	out, err := s.run(sid, asRoot+scheduleScript)
	if err != nil {
		return ServerSchedule{}, err
	}
	return parseSchedule(out), nil
}

// Backup finds the newest file under dir, to tell how old the last backup is.
func (s *ServerService) Backup(sid, dir string) (ServerBackup, error) {
	if !domain.ValidServerPath(dir) {
		return ServerBackup{}, errors.New("that is not a folder path")
	}
	find := fmt.Sprintf(`find "%s" -type f -printf '%%T@ %%s %%P\n' 2>/dev/null | sort -rn | head -1`, dir)
	out, err := s.runTimeout(sid, asRoot+fmt.Sprintf(`echo '@@@now'; date +%%s
R=$(%[1]s); [ -n "$R" ] || R=$($SUDO %[1]s)
echo '@@@newest'; echo "$R"
true`, find), 30*time.Second)
	if err != nil {
		return ServerBackup{}, err
	}
	return parseBackup(out), nil
}

const fleetScript = overviewScript + `
echo '##failed'; systemctl list-units --type=service --state=failed --plain --no-legend --no-pager 2>/dev/null | wc -l
echo '##certs'; $SUDO find /etc/letsencrypt/live -mindepth 2 -maxdepth 2 -name cert.pem 2>/dev/null | while read -r f; do $SUDO openssl x509 -in "$f" -noout -enddate 2>/dev/null; done
echo '##now'; date +%s
`

// FleetHost looks at one saved host on a connection of its own that never asks a question (a host key that is not trusted yet
// is an error, like any other). The host not answering is no Go error: it is in Error. Only an unknown host is.
func (s *ServerService) FleetHost(hostID string) (ServerFleetHost, error) {
	if _, err := s.c.Store.GetHost(hostID); err != nil {
		return ServerFleetHost{}, err
	}
	res := ServerFleetHost{HostID: hostID}
	start := time.Now()
	cl, _, err := s.c.connectQuiet(hostID)
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	defer cl.Close()
	res.LatencyMs = time.Since(start).Milliseconds()
	out, err := runScript(cl, asRoot+fleetScript, 15*time.Second)
	if err != nil && strings.TrimSpace(out) == "" {
		res.Error = err.Error()
		return res, nil
	}
	res.Overview, res.FailedUnits = parseFleet(out)
	res.CertDays = parseFleetCertDays(out)
	return res, nil
}
