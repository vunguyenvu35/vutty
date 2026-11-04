package usecase

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// ServerOverview is what the Server section shows first. Rates (CPU, network) need two samples: the CPU is sampled twice
// by the script itself, the network by two calls of Overview, so NetRx and NetTx are 0 on the first one.
type ServerOverview struct {
	OS        string     `json:"os"`
	Kernel    string     `json:"kernel"`
	Cores     int        `json:"cores"`
	UptimeSec int64      `json:"uptime_sec"`
	CPUPct    float64    `json:"cpu_pct"`
	Load      [3]float64 `json:"load"`
	MemTotal  uint64     `json:"mem_total"` // bytes
	MemUsed   uint64     `json:"mem_used"`
	SwapTotal uint64     `json:"swap_total"`
	SwapUsed  uint64     `json:"swap_used"`
	DiskTotal uint64     `json:"disk_total"` // the root file system
	DiskUsed  uint64     `json:"disk_used"`
	NetRx     float64    `json:"net_rx"` // bytes per second
	NetTx     float64    `json:"net_tx"`
	Root      bool       `json:"root"` // the login is root: actions need no sudo
}

// ServerProcess is one line of ps.
type ServerProcess struct {
	PID     int     `json:"pid"`
	User    string  `json:"user"`
	CPU     float64 `json:"cpu"`
	Mem     float64 `json:"mem"`
	Command string  `json:"command"`
}

// ServerUnit is a systemd service.
type ServerUnit struct {
	Name        string `json:"name"` // without ".service"
	Description string `json:"description"`
	Active      string `json:"active"`  // active, inactive, failed, activating…
	Sub         string `json:"sub"`     // running, exited, dead, failed…
	Enabled     string `json:"enabled"` // enabled, disabled, static… ("" when unknown)
}

// ServerContainer is a Docker container.
type ServerContainer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"` // running, exited, paused…
	Status  string `json:"status"`
	Ports   string `json:"ports"`
	Project string `json:"project"` // the Docker Compose project it belongs to ("" when Compose did not start it)
}

// ServerContainers is the Docker section: Installed is false when the server has no docker command.
type ServerContainers struct {
	Installed  bool              `json:"installed"`
	Containers []ServerContainer `json:"containers"`
}

// section returns what the script printed after the "##name" line, up to the next marker.
func section(out, name string) string {
	var b strings.Builder
	in := false
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "##") {
			in = strings.TrimSpace(ln[2:]) == name
			continue
		}
		if in {
			b.WriteString(ln)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// cpuTimes reads a "cpu  user nice system idle iowait irq softirq steal …" line of /proc/stat: the total and idle ticks.
func cpuTimes(line string) (total, idle uint64, ok bool) {
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, false
	}
	for i, s := range f[1:] {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		if i < 8 { // guest time is already counted in user
			total += v
		}
		if i == 3 || i == 4 { // idle + iowait
			idle += v
		}
	}
	return total, idle, true
}

// netTotals sums the bytes received and sent on every interface but lo, from /proc/net/dev.
func netTotals(dev string) (rx, tx uint64) {
	for _, ln := range strings.Split(dev, "\n") {
		name, rest, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "lo" {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		r, e1 := strconv.ParseUint(f[0], 10, 64)
		x, e2 := strconv.ParseUint(f[8], 10, 64)
		if e1 == nil && e2 == nil {
			rx += r
			tx += x
		}
	}
	return rx, tx
}

// parseOverview turns the output of overviewScript into an overview; net is the byte totals, which the caller turns into a rate.
func parseOverview(out string) (o ServerOverview, rx, tx uint64) {
	cpu := strings.Split(section(out, "cpu"), "\n")
	if len(cpu) >= 2 {
		t1, i1, ok1 := cpuTimes(cpu[0])
		t2, i2, ok2 := cpuTimes(cpu[1])
		if ok1 && ok2 && t2 > t1 {
			o.CPUPct = round1(100 * float64((t2-t1)-(i2-i1)) / float64(t2-t1))
		}
	}
	kb := map[string]uint64{}
	for _, ln := range strings.Split(section(out, "mem"), "\n") {
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		if f := strings.Fields(v); len(f) > 0 {
			n, _ := strconv.ParseUint(f[0], 10, 64)
			kb[k] = n * 1024
		}
	}
	o.MemTotal = kb["MemTotal"]
	if kb["MemAvailable"] <= o.MemTotal {
		o.MemUsed = o.MemTotal - kb["MemAvailable"]
	}
	o.SwapTotal = kb["SwapTotal"]
	if kb["SwapFree"] <= o.SwapTotal {
		o.SwapUsed = o.SwapTotal - kb["SwapFree"]
	}
	if f := strings.Fields(section(out, "disk")); len(f) >= 4 { // df -Pk: fs, 1K-blocks, used, available…
		total, _ := strconv.ParseUint(f[1], 10, 64)
		used, _ := strconv.ParseUint(f[2], 10, 64)
		o.DiskTotal, o.DiskUsed = total*1024, used*1024
	}
	for i, s := range strings.Fields(section(out, "load")) {
		if i < 3 {
			o.Load[i], _ = strconv.ParseFloat(s, 64)
		}
	}
	if f := strings.Fields(section(out, "up")); len(f) > 0 {
		s, _ := strconv.ParseFloat(f[0], 64)
		o.UptimeSec = int64(s)
	}
	info := strings.Split(section(out, "os"), "\n")
	if len(info) >= 3 {
		o.OS, o.Kernel = strings.TrimSpace(info[0]), strings.TrimSpace(info[1])
		o.Cores, _ = strconv.Atoi(strings.TrimSpace(info[2]))
	}
	o.Root = strings.TrimSpace(section(out, "uid")) == "0"
	rx, tx = netTotals(section(out, "net"))
	return o, rx, tx
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

// parseProcesses reads `ps -eo pid=,user=,pcpu=,pmem=,args=` and returns the busiest first.
func parseProcesses(out string, max int) []ServerProcess {
	var ps []ServerProcess
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 5 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		cpu, _ := strconv.ParseFloat(f[2], 64)
		mem, _ := strconv.ParseFloat(f[3], 64)
		cmd := strings.Join(f[4:], " ")
		if len(cmd) > 200 {
			cmd = cmd[:200]
		}
		ps = append(ps, ServerProcess{PID: pid, User: f[1], CPU: cpu, Mem: mem, Command: strings.ToValidUTF8(cmd, "")})
	}
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].CPU != ps[j].CPU {
			return ps[i].CPU > ps[j].CPU
		}
		return ps[i].Mem > ps[j].Mem
	})
	if max > 0 && len(ps) > max {
		ps = ps[:max]
	}
	return ps
}

// parseUnits joins `systemctl list-units --type=service --all --plain --no-legend` ("unit load active sub description…")
// with `systemctl list-unit-files --type=service --plain --no-legend` ("unit state…").
func parseUnits(out string) []ServerUnit {
	enabled := map[string]string{}
	for _, ln := range strings.Split(section(out, "files"), "\n") {
		if f := strings.Fields(ln); len(f) >= 2 {
			enabled[strings.TrimSuffix(f[0], ".service")] = f[1]
		}
	}
	var us []ServerUnit
	for _, ln := range strings.Split(section(out, "units"), "\n") {
		f := strings.Fields(strings.TrimLeft(ln, "●* "))
		if len(f) < 4 || !strings.HasSuffix(f[0], ".service") || f[1] == "not-found" {
			continue
		}
		name := strings.TrimSuffix(f[0], ".service")
		us = append(us, ServerUnit{Name: name, Active: f[2], Sub: f[3], Description: strings.Join(f[4:], " "), Enabled: enabled[name]})
	}
	sort.SliceStable(us, func(i, j int) bool { // failed first, then running, then the rest by name
		ri, rj := unitRank(us[i]), unitRank(us[j])
		if ri != rj {
			return ri < rj
		}
		return us[i].Name < us[j].Name
	})
	return us
}

func unitRank(u ServerUnit) int {
	switch {
	case u.Active == "failed":
		return 0
	case u.Active == "active" && u.Sub == "running":
		return 1
	case u.Active == "active":
		return 2
	}
	return 3
}

// parseContainers reads `docker ps -a --format '{{json .}}'`, one object per line.
func parseContainers(out string) ServerContainers {
	if strings.Contains(out, "##nodocker") {
		return ServerContainers{}
	}
	res := ServerContainers{Installed: true, Containers: []ServerContainer{}}
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "{") {
			continue
		}
		var j struct{ ID, Names, Image, State, Status, Ports, Labels string }
		if json.Unmarshal([]byte(ln), &j) != nil {
			continue
		}
		res.Containers = append(res.Containers, ServerContainer{ID: j.ID, Name: j.Names, Image: j.Image, State: j.State, Status: j.Status, Ports: j.Ports, Project: composeProject(j.Labels)})
	}
	sort.SliceStable(res.Containers, func(i, j int) bool {
		a, b := res.Containers[i], res.Containers[j]
		if (a.State == "running") != (b.State == "running") {
			return a.State == "running"
		}
		return a.Name < b.Name
	})
	return res
}

// ServerPort is a TCP port something listens on.
type ServerPort struct {
	Port    int    `json:"port"`
	Process string `json:"process"` // "" when the login may not see it
	Local   bool   `json:"local"`   // listens on the loopback address only
	Open    bool   `json:"open"`    // the firewall lets it in (true as well when there is no firewall to say otherwise)
}

// ServerPorts is the Ports section. Firewall is "active" or "inactive" for ufw, "unknown" when there is no ufw or the login may not ask it.
type ServerPorts struct {
	Firewall string       `json:"firewall"`
	Ports    []ServerPort `json:"ports"`
}

// parsePorts reads `ss -tlnpH` (state recv-q send-q local peer [users:(("name",pid=1,fd=3))]) and `ufw status`.
func parsePorts(out string) ServerPorts {
	res := ServerPorts{Firewall: "unknown", Ports: []ServerPort{}}
	allowed := map[int]bool{}
	switch ufw := section(out, "ufw"); {
	case strings.Contains(ufw, "Status: active"):
		res.Firewall = "active"
		for _, ln := range strings.Split(ufw, "\n") {
			f := strings.Fields(ln)
			if len(f) < 3 || f[1] != "ALLOW" {
				continue
			}
			for _, p := range strings.Split(strings.SplitN(f[0], "/", 2)[0], ",") {
				if n, err := strconv.Atoi(p); err == nil {
					allowed[n] = true
				}
			}
		}
	case strings.Contains(ufw, "Status: inactive"):
		res.Firewall = "inactive"
	}
	seen := map[int]int{} // port -> index
	for _, ln := range strings.Split(section(out, "ss"), "\n") {
		f := strings.Fields(ln)
		if len(f) < 5 || f[0] != "LISTEN" {
			continue
		}
		i := strings.LastIndex(f[3], ":")
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(f[3][i+1:])
		if err != nil {
			continue
		}
		host := strings.Trim(f[3][:i], "[]")
		local := host == "127.0.0.1" || host == "::1" || strings.HasPrefix(host, "127.")
		proc := ""
		if len(f) > 5 {
			if a := strings.Index(f[5], `(("`); a >= 0 {
				if b := strings.Index(f[5][a+3:], `"`); b >= 0 {
					proc = f[5][a+3 : a+3+b]
				}
			}
		}
		if j, ok := seen[port]; ok { // the same port on IPv4 and IPv6, or on several addresses: reachable if any of them is
			res.Ports[j].Local = res.Ports[j].Local && local
			if res.Ports[j].Process == "" {
				res.Ports[j].Process = proc
			}
			continue
		}
		seen[port] = len(res.Ports)
		res.Ports = append(res.Ports, ServerPort{Port: port, Process: proc, Local: local, Open: res.Firewall != "active" || allowed[port]})
	}
	sort.SliceStable(res.Ports, func(i, j int) bool { return res.Ports[i].Port < res.Ports[j].Port })
	return res
}
