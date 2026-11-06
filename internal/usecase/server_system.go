package usecase

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The Maintenance section also shows and changes three basic settings of the machine: its hostname, its time zone, and the swap file.
// Only the file /swapfile is ever created or removed here; a swap partition is shown and left alone.

// ServerSystem is the System box of Maintenance.
type ServerSystem struct {
	Hostname  string `json:"hostname"`
	Timezone  string `json:"timezone"`
	Systemd   bool   `json:"systemd"`    // hostnamectl and timedatectl are there
	SwapTotal int64  `json:"swap_total"` // bytes, all swap areas
	SwapFile  int64  `json:"swap_file"`  // bytes of /swapfile when it is in use, else 0
	SwapOther bool   `json:"swap_other"` // some swap that is not /swapfile
	RAM       int64  `json:"ram"`        // bytes
	DiskFree  int64  `json:"disk_free"`  // bytes free on / (what a new swap file may take)
}

var (
	hostnameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	zoneRe     = regexp.MustCompile(`^[A-Za-z0-9_+/-]{1,64}$`)
)

const systemScript = `echo '##host'; hostname
echo '##tz'; timedatectl show -p Timezone --value 2>/dev/null || cat /etc/timezone 2>/dev/null
echo '##systemd'; command -v hostnamectl >/dev/null 2>&1 && command -v timedatectl >/dev/null 2>&1 && echo yes
echo '##swap'; swapon --show --noheadings --bytes 2>/dev/null
echo '##ram'; grep -E '^MemTotal:' /proc/meminfo
echo '##disk'; df -Pk / | tail -1`

// parseSystem reads the output of systemScript.
func parseSystem(out string) ServerSystem {
	var r ServerSystem
	r.Hostname = strings.TrimSpace(section(out, "host"))
	r.Timezone = strings.TrimSpace(section(out, "tz"))
	r.Systemd = strings.TrimSpace(section(out, "systemd")) == "yes"
	for _, ln := range strings.Split(section(out, "swap"), "\n") {
		f := strings.Fields(ln) // NAME TYPE SIZE USED PRIO
		if len(f) < 3 {
			continue
		}
		n, _ := strconv.ParseInt(f[2], 10, 64)
		r.SwapTotal += n
		if f[0] == "/swapfile" {
			r.SwapFile = n
		} else {
			r.SwapOther = true
		}
	}
	if f := strings.Fields(section(out, "ram")); len(f) >= 2 {
		kb, _ := strconv.ParseInt(f[1], 10, 64)
		r.RAM = kb * 1024
	}
	if f := strings.Fields(section(out, "disk")); len(f) >= 4 {
		kb, _ := strconv.ParseInt(f[3], 10, 64)
		r.DiskFree = kb * 1024
	}
	return r
}

// System reports the hostname, the time zone and the swap.
func (s *ServerService) System(sid string) (ServerSystem, error) {
	out, err := s.run(sid, systemScript)
	if err != nil {
		return ServerSystem{}, err
	}
	return parseSystem(out), nil
}

// SetHostname changes the hostname (the static one, which survives a reboot).
func (s *ServerService) SetHostname(sid, name string) error {
	name = strings.TrimSpace(name)
	if !hostnameRe.MatchString(name) {
		return errors.New("a hostname is lower-case letters, digits and hyphens, up to 63 characters")
	}
	_, err := s.run(sid, asRoot+`command -v hostnamectl >/dev/null 2>&1 || { echo "hostnamectl is not available on this server" >&2; exit 1; }
$SUDO hostnamectl set-hostname `+name+` 2>&1`)
	s.audit(sid, "set the hostname to "+name, err)
	return err
}

// SetTimezone changes the time zone, after checking the name against the list of the server.
func (s *ServerService) SetTimezone(sid, zone string) error {
	zone = strings.TrimSpace(zone)
	if !zoneRe.MatchString(zone) {
		return errors.New("that is not a time zone name, such as Asia/Ho_Chi_Minh or UTC")
	}
	_, err := s.run(sid, asRoot+fmt.Sprintf(`command -v timedatectl >/dev/null 2>&1 || { echo "timedatectl is not available on this server" >&2; exit 1; }
timedatectl list-timezones 2>/dev/null | grep -Fxq %s || { echo "the server does not know the time zone %s" >&2; exit 1; }
$SUDO timedatectl set-timezone %s 2>&1`, zone, zone, zone))
	s.audit(sid, "set the time zone to "+zone, err)
	return err
}

const minSwapMB, maxSwapMB = 256, 32768

// SwapCreate creates /swapfile of this many megabytes, turns it on, and keeps it for the next boot (fstab). It refuses when there is
// a /swapfile already or when the disk would be left with less than 1 GB.
func (s *ServerService) SwapCreate(sid string, mb int) error {
	if mb < minSwapMB || mb > maxSwapMB {
		return fmt.Errorf("the swap file is %d MB to %d MB", minSwapMB, maxSwapMB)
	}
	script := asRoot + fmt.Sprintf(`MB=%d
[ -e /swapfile ] && { echo "/swapfile exists already" >&2; exit 1; }
FREE=$(df -Pk / | tail -1 | awk '{print int($4/1024)}')
[ "$FREE" -gt $((MB + 1024)) ] || { echo "not enough free disk: $FREE MB free, and 1 GB must stay free" >&2; exit 1; }
{ $SUDO fallocate -l ${MB}M /swapfile 2>/dev/null || $SUDO dd if=/dev/zero of=/swapfile bs=1M count=$MB status=none; } || { $SUDO rm -f /swapfile; echo "could not create the file" >&2; exit 1; }
$SUDO chmod 600 /swapfile
$SUDO mkswap /swapfile >/dev/null || { $SUDO rm -f /swapfile; echo "mkswap failed" >&2; exit 1; }
$SUDO swapon /swapfile || { $SUDO rm -f /swapfile; echo "swapon failed" >&2; exit 1; }
grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' | $SUDO tee -a /etc/fstab >/dev/null`, mb)
	_, err := s.runTimeout(sid, script, serverLongTimeout)
	s.audit(sid, fmt.Sprintf("create a %d MB swap file", mb), err)
	return err
}

// SwapRemove turns /swapfile off and deletes it (its line of fstab goes too). Refused when the memory in use would not fit in the RAM left.
func (s *ServerService) SwapRemove(sid string) error {
	script := asRoot + `[ -f /swapfile ] || { echo "there is no /swapfile" >&2; exit 1; }
USED=$(swapon --show --noheadings --bytes 2>/dev/null | awk '$1=="/swapfile"{print int($4/1048576)}')
AVAIL=$(awk '/^MemAvailable:/{print int($2/1024)}' /proc/meminfo)
[ -z "$USED" ] || [ "${AVAIL:-0}" -gt $((USED + 256)) ] || { echo "the swap file holds ${USED} MB and only ${AVAIL} MB of RAM is free: removing it could run the server out of memory" >&2; exit 1; }
$SUDO swapoff /swapfile 2>/dev/null; true
$SUDO rm -f /swapfile
$SUDO sed -i '\|^/swapfile |d' /etc/fstab`
	_, err := s.runTimeout(sid, script, serverLongTimeout)
	s.audit(sid, "remove the swap file", err)
	return err
}
