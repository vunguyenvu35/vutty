package usecase

import (
	"strings"
	"testing"
)

const sampleOverview = `##cpu
cpu  1000 0 500 8000 500 0 0 0 0 0
cpu  1100 0 550 8300 550 0 0 0 0 0
##mem
MemTotal:        8000000 kB
MemAvailable:    2000000 kB
SwapTotal:       2000000 kB
SwapFree:        1500000 kB
##disk
/dev/sda1 51200000 40960000 10240000 80% /
##load
0.84 0.71 0.66 1/200 1234
##up
3542400.55 1000.00
##os
Ubuntu 24.04 LTS
6.8.0-45-generic
4
##uid
1000
##net
Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 5000 10 0 0 0 0 0 0 5000 10 0 0 0 0 0 0
  eth0: 1000 10 0 0 0 0 0 0 2000 10 0 0 0 0 0 0
  eth1:  100  1 0 0 0 0 0 0  200  1 0 0 0 0 0 0
`

func TestParseOverview(t *testing.T) {
	o, rx, tx := parseOverview(sampleOverview)
	// busy = (1100+550+... ) total delta 500 (1100+550+8300+550 - 1000-500-8000-500 = 500), idle delta 350 -> 30% busy
	if o.CPUPct != 30 {
		t.Fatalf("cpu %v", o.CPUPct)
	}
	if o.MemTotal != 8000000*1024 || o.MemUsed != 6000000*1024 || o.SwapUsed != 500000*1024 {
		t.Fatalf("%+v", o)
	}
	if o.DiskTotal != 51200000*1024 || o.DiskUsed != 40960000*1024 {
		t.Fatalf("disk %+v", o)
	}
	if o.Load != [3]float64{0.84, 0.71, 0.66} || o.UptimeSec != 3542400 || o.Cores != 4 || o.OS != "Ubuntu 24.04 LTS" || o.Kernel != "6.8.0-45-generic" || o.Root {
		t.Fatalf("%+v", o)
	}
	if rx != 1100 || tx != 2200 { // lo is left out
		t.Fatalf("net %d %d", rx, tx)
	}
}

func TestParseOverviewSurvivesAShortOrEmptyOutput(t *testing.T) {
	o, rx, tx := parseOverview("")
	if o.CPUPct != 0 || o.MemTotal != 0 || rx != 0 || tx != 0 {
		t.Fatalf("%+v", o)
	}
	o, _, _ = parseOverview("##mem\nMemTotal: 100 kB\nMemAvailable: 999 kB\n") // available above total: no underflow
	if o.MemUsed != 0 {
		t.Fatalf("%d", o.MemUsed)
	}
}

func TestParseProcessesSortsByCPUAndKeepsTheWholeCommand(t *testing.T) {
	out := "  1 root 0.0 0.1 /sbin/init splash\n2210 app 38.5 12.4 node server.js --port 3000\nbad line\n3011 redis 1.3 2.6 redis-server *:6379\n"
	ps := parseProcesses(out, 2)
	if len(ps) != 2 || ps[0].PID != 2210 || ps[0].Command != "node server.js --port 3000" || ps[1].PID != 3011 {
		t.Fatalf("%+v", ps)
	}
}

func TestParseProcessesLeavesOutThePanelsOwnPs(t *testing.T) {
	out := "3997644 root 100.0 0.1 ps -eo pid=,user=,pcpu=,pmem=,args=\n3997643 root 0.0 0.0 bash -c LC_ALL=C; export LC_ALL; ps -eo pid=,user=,pcpu=,pmem=,args=\n" +
		"2210 app 38.5 12.4 node server.js\n4001 app 9.0 0.1 ps -eo pid=,user=,pcpu=,pmem=,args= --other\n4002 app 1.0 0.1 sh -c ps -eo pid=,user=,pcpu=,pmem=,args=\n"
	ps := parseProcesses(out, 10)
	var pids []int
	for _, p := range ps {
		pids = append(pids, p.PID)
	}
	// only the panel's own ps and its shell go: a ps somebody else runs (other flags, another wrapper) stays
	if len(pids) != 3 || pids[0] != 2210 || pids[1] != 4001 || pids[2] != 4002 {
		t.Fatalf("%v", pids)
	}
}

func TestParseUnitsJoinsEnabledStateAndPutsFailedFirst(t *testing.T) {
	out := `##units
  nginx.service        loaded active running A high performance web server
  app-worker.service   loaded failed failed  Job queue
● ghost.service        not-found inactive dead ghost.service
  cron.service         loaded active running Regular background program processing daemon
##files
nginx.service enabled enabled
app-worker.service enabled enabled
cron.service enabled enabled
`
	us := parseUnits(out)
	if len(us) != 3 {
		t.Fatalf("%+v", us)
	}
	if us[0].Name != "app-worker" || us[0].Active != "failed" || us[0].Enabled != "enabled" || us[0].Description != "Job queue" {
		t.Fatalf("%+v", us[0])
	}
	if us[1].Name != "cron" || us[2].Name != "nginx" || us[2].Description != "A high performance web server" {
		t.Fatalf("%+v", us)
	}
}

func TestParseContainers(t *testing.T) {
	out := `{"ID":"a1","Image":"registry/api:1.8","Names":"api","Ports":"0.0.0.0:3000->3000/tcp","State":"running","Status":"Up 2 hours"}
{"ID":"b2","Image":"registry/batch","Names":"old-batch","Ports":"","State":"exited","Status":"Exited (0) 3 days ago"}
`
	c := parseContainers(out)
	if !c.Installed || len(c.Containers) != 2 || c.Containers[0].Name != "api" || c.Containers[1].State != "exited" {
		t.Fatalf("%+v", c)
	}
	if c := parseContainers("##nodocker\n"); c.Installed || len(c.Containers) != 0 {
		t.Fatalf("%+v", c)
	}
}

func (h *harness) serverSession(t *testing.T, f func(cmd string) (string, string, int)) (string, *[]string) {
	t.Helper()
	var cmds []string
	host := h.sshHost(t, "srv", execServer(t, func(cmd string) (string, string, int) { cmds = append(cmds, cmd); return f(cmd) }), "")
	type res struct {
		info ServerInfo
		err  error
	}
	done := make(chan res, 1)
	go func() { i, err := h.app.Server.Open(host.ID, "srv-key"); done <- res{i, err} }()
	h.trust(t, "srv-key")
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	t.Cleanup(func() { _ = h.app.Server.Close(r.info.SID) })
	return r.info.SID, &cmds
}

func TestServerServiceRunsFixedCommandsAndRefusesOddNames(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, cmds := h.serverSession(t, func(cmd string) (string, string, int) {
		switch {
		case strings.Contains(cmd, "/proc/stat"):
			return sampleOverview, "", 0
		case strings.Contains(cmd, "systemctl restart"):
			return "", "Failed to restart: Unit not found.\n", 5
		case strings.Contains(cmd, "ps -eo"):
			return "10 root 1.0 0.5 /bin/x\n", "", 0
		}
		return "", "", 0
	})
	o, err := h.app.Server.Overview(sid)
	if err != nil || o.Cores != 4 || o.NetRx != 0 { // the first call has no rate yet
		t.Fatalf("%+v %v", o, err)
	}
	o, err = h.app.Server.Overview(sid)
	if err != nil || o.NetRx != 0 { // the same totals again: no traffic in between
		t.Fatalf("%+v %v", o, err)
	}
	ps, err := h.app.Server.Processes(sid)
	if err != nil || len(ps) != 1 || ps[0].PID != 10 {
		t.Fatalf("%+v %v", ps, err)
	}
	// a failing command returns what the server said, not a bare exit status
	err = h.app.Server.ServiceAction(sid, "nginx", "restart")
	if err == nil || !strings.Contains(err.Error(), "Unit not found") {
		t.Fatalf("%v", err)
	}
	before := len(*cmds)
	for _, bad := range []string{"", "a b", "x;reboot", "$(id)", "-rf", "a`id`"} {
		if err := h.app.Server.ServiceAction(sid, bad, "restart"); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
		if err := h.app.Server.ContainerAction(sid, bad, "stop"); err == nil {
			t.Fatalf("%q was accepted for a container", bad)
		}
		if _, err := h.app.Server.Logs(sid, "service", bad); err == nil && bad != "" {
			t.Fatalf("%q was accepted for logs", bad)
		}
	}
	if err := h.app.Server.ServiceAction(sid, "nginx", "reboot"); err == nil {
		t.Fatal("an unknown action was accepted")
	}
	if err := h.app.Server.Kill(sid, 1, "TERM"); err == nil {
		t.Fatal("init can be killed")
	}
	if err := h.app.Server.Kill(sid, 42, "HUP; reboot"); err == nil {
		t.Fatal("an odd signal was accepted")
	}
	if len(*cmds) != before {
		t.Fatalf("a refused call reached the server: %v", (*cmds)[before:])
	}
	if err := h.app.Server.Kill(sid, 42, "KILL"); err != nil {
		t.Fatal(err)
	}
	if last := (*cmds)[len(*cmds)-1]; !strings.Contains(last, "$SUDO kill -KILL 42") {
		t.Fatalf("%q", last)
	}
	if err := h.app.Server.Close(sid); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Server.Overview(sid); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("%v", err)
	}
}

func TestParsePortsJoinsTheFirewallAndMergesIPv4WithIPv6(t *testing.T) {
	out := `##ss
LISTEN 0 4096 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=1,fd=3))
LISTEN 0 4096 [::]:22 [::]:* users:(("sshd",pid=1,fd=4))
LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("nginx",pid=9,fd=6))
LISTEN 0 200 127.0.0.1:5432 0.0.0.0:*
LISTEN 0 4096 *:9000 *:* users:(("minio",pid=7,fd=8))
##ufw
Status: active

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere
80,443/tcp                 ALLOW       Anywhere
22/tcp (v6)                ALLOW       Anywhere (v6)
`
	p := parsePorts(out)
	if p.Firewall != "active" || len(p.Ports) != 4 {
		t.Fatalf("%+v", p)
	}
	want := []ServerPort{{22, "sshd", false, true}, {80, "nginx", false, true}, {5432, "", true, false}, {9000, "minio", false, false}}
	for i, w := range want {
		if p.Ports[i] != w {
			t.Fatalf("%d: %+v, want %+v", i, p.Ports[i], w)
		}
	}
	// no firewall to say otherwise: every port is open
	q := parsePorts("##ss\nLISTEN 0 1 0.0.0.0:8080 0.0.0.0:*\n##ufw\n")
	if q.Firewall != "unknown" || len(q.Ports) != 1 || !q.Ports[0].Open {
		t.Fatalf("%+v", q)
	}
	if r := parsePorts("##ufw\nStatus: inactive\n"); r.Firewall != "inactive" || len(r.Ports) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestDockerParsers(t *testing.T) {
	st := parseDockerStats(`{"Name":"db","CPUPerc":"0.50%","MemPerc":"10.00%","MemUsage":"200MiB / 2GiB"}
{"Name":"api","CPUPerc":"12.30%","MemPerc":"3.10%","MemUsage":"60MiB / 2GiB"}
`)
	if len(st) != 2 || st[0].Name != "api" || st[0].CPU != 12.3 || st[1].MemUsage != "200MiB" {
		t.Fatalf("%+v", st)
	}
	in, err := parseDockerInspect(`[{"Name":"/api","Created":"2026-01-01","Path":"node","Args":["app.js"],"State":{"Status":"running"},
"Config":{"Image":"api:1","Env":["A=1","TOKEN=x=y"],"Labels":{"k":"v"}},"HostConfig":{"RestartPolicy":{"Name":""}},
"Mounts":[{"Type":"volume","Source":"/v","Destination":"/data","RW":true}],
"NetworkSettings":{"Ports":{"3000/tcp":[{"HostIp":"0.0.0.0","HostPort":"3000"}],"9229/tcp":null},"Networks":{"bridge":{}}}}]`)
	if err != nil || in.Name != "api" || in.Restart != "no" || in.Command != "node app.js" || len(in.Env) != 2 || in.Env[1].Value != "x=y" || len(in.Ports) != 2 || in.Mounts[0].Dest != "/data" {
		t.Fatalf("%+v %v", in, err)
	}
	if _, err := parseDockerInspect("Error: No such object"); err == nil {
		t.Fatal("no JSON was accepted")
	}
	o := parseDockerObjects(`##used
{"Image":"api:1","Mounts":"pgdata,/etc/x"}
##images
{"ID":"aa11bb22","Repository":"api","Tag":"1","Size":"100MB","CreatedSince":"2 days ago"}
{"ID":"cc33dd44","Repository":"<none>","Tag":"<none>","Size":"5MB","CreatedSince":"1 month ago"}
##volumes
{"Name":"pgdata","Driver":"local"}
{"Name":"old","Driver":"local"}
##networks
{"ID":"n1","Name":"bridge","Driver":"bridge","Scope":"local"}
`)
	if len(o.Images) != 2 || !o.Images[1].InUse && o.Images[1].Name == "api:1" || o.Images[0].Name != "<none>" || o.Images[0].InUse {
		t.Fatalf("%+v", o.Images)
	}
	if !o.Volumes[1].InUse || o.Volumes[0].InUse || len(o.Networks) != 1 {
		t.Fatalf("%+v", o)
	}
}

func TestDockerCommandsRefuseOddNames(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, cmds := h.serverSession(t, func(cmd string) (string, string, int) { return "", "", 0 })
	before := len(*cmds)
	for _, bad := range []string{"", "a b", "x;reboot", "$(id)", "-rf"} {
		if _, err := h.app.Server.DockerShellCommand(sid, bad); err == nil {
			t.Fatalf("%q was accepted for a shell", bad)
		}
		if _, err := h.app.Server.DockerInspect(sid, bad); err == nil {
			t.Fatalf("%q was accepted for inspect", bad)
		}
		if err := h.app.Server.DockerRemove(sid, "volume", bad); err == nil {
			t.Fatalf("%q was accepted as a volume", bad)
		}
		if err := h.app.Server.DockerRemove(sid, "image", bad); err == nil {
			t.Fatalf("%q was accepted as an image", bad)
		}
	}
	if err := h.app.Server.DockerRemove(sid, "network", "bridge"); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	if len(*cmds) != before {
		t.Fatalf("a refused call reached the server: %v", (*cmds)[before:])
	}
	line, err := h.app.Server.DockerShellCommand(sid, "api")
	if err != nil || !strings.Contains(line, "exec -it api sh -c") {
		t.Fatalf("%q %v", line, err)
	}
}

func TestParseUsers(t *testing.T) {
	out := `##passwd
root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
deploy:x:1001:1001::/home/deploy:/bin/bash
ubuntu:x:1000:1000:Ubuntu:/home/ubuntu:/bin/bash
www:x:1002:1002::/var/www:/usr/sbin/nologin
nobody:x:65534:65534:nobody:/nonexistent:/bin/bash
##groups
sudo:x:27:ubuntu
admin:x:110:
##keys
root 1
ubuntu 2
deploy -1
##who
ubuntu   pts/0        2026-11-05 10:11 (203.0.113.7)
root     tty1         2026-11-05 09:00
##last
root                             pts/0    1.2.3.4          Thu Nov  5 09:00:00 +0700 2026
ubuntu                           pts/1    1.2.3.4          Wed Nov  4 20:10:11 +0700 2026
deploy                                                     **Never logged in**
`
	u := parseUsers(out)
	if len(u.Users) != 3 || u.Users[0].Name != "root" || u.Users[1].Name != "ubuntu" || u.Users[2].Name != "deploy" {
		t.Fatalf("%+v", u.Users)
	}
	if !u.Users[0].Sudo || !u.Users[1].Sudo || u.Users[2].Sudo || u.Users[1].Keys != 2 || u.Users[2].Keys != -1 {
		t.Fatalf("%+v", u.Users)
	}
	if u.Users[1].Login == "" || u.Users[2].Login != "" {
		t.Fatalf("login: %+v", u.Users)
	}
	if len(u.Sessions) != 2 || u.Sessions[0].From != "203.0.113.7" || u.Sessions[1].From != "" || u.Sessions[1].Since != "2026-11-05 09:00" {
		t.Fatalf("%+v", u.Sessions)
	}
}

func TestServerAdminChecksBeforeItTouchesTheServer(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, cmds := h.serverSession(t, func(cmd string) (string, string, int) { return "", "", 0 })
	x, err := h.app.Server.get(sid)
	if err != nil || x.login == "" || x.sshPort == 0 {
		t.Fatalf("%+v %v", x, err)
	}
	good := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl laptop"
	fp := "SHA256:" + strings.Repeat("a", 43)
	before := len(*cmds)
	for _, bad := range []string{"", "Root", "a b", "x;reboot", "$(id)", "-rf", "a/b"} {
		if _, err := h.app.Server.SSHKeys(sid, bad); err == nil {
			t.Fatalf("%q accepted for keys", bad)
		}
		if err := h.app.Server.AddSSHKey(sid, bad, good); err == nil {
			t.Fatalf("%q accepted for a new key", bad)
		}
		if err := h.app.Server.RemoveSSHKey(sid, bad, fp); err == nil {
			t.Fatalf("%q accepted for a key removal", bad)
		}
		if err := h.app.Server.AddUser(sid, bad, false); err == nil {
			t.Fatalf("%q accepted as a new account", bad)
		}
		if err := h.app.Server.DeleteUser(sid, bad, true); err == nil {
			t.Fatalf("%q accepted for deletion", bad)
		}
	}
	for _, key := range []string{"", "not a key", good + "\nssh-rsa AAAA x", "command=\"rm -rf /\" " + good, "ssh-ed25519 AAAA$(id)"} {
		if err := h.app.Server.AddSSHKey(sid, "deploy", key); err == nil {
			t.Fatalf("%q accepted as a key", key)
		}
	}
	for _, f := range []string{"", "SHA256:short", fp + ";id", "MD5:aa"} {
		if err := h.app.Server.RemoveSSHKey(sid, "deploy", f); err == nil {
			t.Fatalf("%q accepted as a fingerprint", f)
		}
	}
	for _, p := range []int{0, -1, 65536} {
		if err := h.app.Server.FirewallAllow(sid, p, "tcp"); err == nil {
			t.Fatalf("port %d accepted", p)
		}
	}
	if err := h.app.Server.FirewallAllow(sid, 80, "icmp"); err == nil {
		t.Fatal("a protocol was accepted")
	}
	if err := h.app.Server.FirewallDelete(sid, x.sshPort, "tcp"); err == nil {
		t.Fatal("the SSH port of the connection can be closed")
	}
	if err := h.app.Server.DeleteUser(sid, "root", false); err == nil {
		t.Fatal("root can be deleted")
	}
	if err := h.app.Server.DeleteUser(sid, x.login, false); err == nil {
		t.Fatal("the account of the panel can be deleted")
	}
	if err := h.app.Server.SetSudo(sid, x.login, false); err == nil {
		t.Fatal("the account of the panel can lose sudo")
	}
	if _, err := h.app.Server.CertRenew(sid, "a b;id", false); err == nil {
		t.Fatal("an odd certificate name was accepted")
	}
	for _, w := range []string{"", "* * *", "0 3 * * * rm", "$(id) * * * *", "0 3 * * *; id"} {
		if err := h.app.Server.CronAdd(sid, w, "echo hi"); err == nil {
			t.Fatalf("%q accepted as a schedule", w)
		}
	}
	for _, c := range []string{"", "echo a\nrm -rf /", strings.Repeat("x", 501)} {
		if err := h.app.Server.CronAdd(sid, "0 3 * * *", c); err == nil {
			t.Fatalf("%q accepted as a command", c)
		}
	}
	if err := h.app.Server.CronRemove(sid, "a\nb"); err == nil {
		t.Fatal("a two-line removal was accepted")
	}
	if len(*cmds) != before {
		t.Fatalf("a refused call reached the server: %v", (*cmds)[before:])
	}
}

func TestServerAdminRunsFixedCommands(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, cmds := h.serverSession(t, func(cmd string) (string, string, int) {
		if strings.Contains(cmd, "ssh-keygen -lf /dev/stdin 2>/dev/null\ndone") {
			return "256 SHA256:" + strings.Repeat("b", 43) + " me@laptop (ED25519)\n", "", 0
		}
		return "", "", 0
	})
	keys, err := h.app.Server.SSHKeys(sid, "deploy")
	if err != nil || len(keys) != 1 || keys[0].Type != "ED25519" || keys[0].Comment != "me@laptop" {
		t.Fatalf("%+v %v", keys, err)
	}
	if err := h.app.Server.FirewallAllow(sid, 8080, "tcp"); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Server.FirewallDelete(sid, 8080, "tcp"); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Server.AddUser(sid, "deploy", true); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Server.CronAdd(sid, "0 3 * * *", "/usr/local/bin/backup.sh"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Server.CertRenew(sid, "example.com", true); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(*cmds, "\n")
	for _, want := range []string{"ufw allow 8080/tcp", "ufw delete allow 8080/tcp", "useradd -m -s /bin/bash deploy", "certbot renew --cert-name example.com --dry-run", "crontab \"$tmp\""} {
		if !strings.Contains(all, want) {
			t.Fatalf("%q was not run:\n%s", want, all)
		}
	}
	if strings.Contains(all, "backup.sh") {
		t.Fatal("the cron command is on the command line: it must travel on the standard input")
	}
}

func TestParseNetworkAndDatabases(t *testing.T) {
	n := parseNetworkPage(`##dev
Inter-|   Receive                                                |  Transmit
    lo: 100 1 0 0 0 0 0 0 100 1 0 0 0 0 0 0
  eth0: 5000 10 0 0 0 0 0 0 7000 12 0 0 0 0 0 0
##addr
1: lo    inet 127.0.0.1/8 scope host lo
2: eth0    inet 10.0.0.2/24 brd 10.0.0.255 scope global eth0
2: eth0    inet6 fe80::1/64 scope link
2: eth0    inet6 2001:db8::2/64 scope global
##conn
0      0      10.0.0.2:22      203.0.113.7:50000 users:(("sshd",pid=10,fd=4))
0      0      10.0.0.2:443     198.51.100.2:41000
`)
	if len(n.Ifaces) != 1 || n.Ifaces[0].RX != 5000 || n.Ifaces[0].TX != 7000 || len(n.Ifaces[0].Addrs) != 2 {
		t.Fatalf("%+v", n.Ifaces)
	}
	if n.Total != 2 || n.Conns[0].Process != "sshd" || n.Conns[1].Process != "" || n.Conns[0].Remote != "203.0.113.7:50000" {
		t.Fatalf("%+v", n)
	}
	d := parseDatabases(`##postgres
version=16.2
conns=7
max=100
db=app|52428800
db=postgres|8000000
##redis
r_redis_version=7.2.4
r_connected_clients=3
r_used_memory_human=1.50M
`)
	if len(d.Engines) != 2 || d.Engines[0].Kind != "postgres" || d.Engines[0].Connections != 7 || d.Engines[0].MaxConns != 100 || len(d.Engines[0].Databases) != 2 || d.Engines[0].Databases[0].Bytes != 52428800 {
		t.Fatalf("%+v", d)
	}
	if d.Engines[1].Version != "7.2.4" || d.Engines[1].Memory != "1.50M" {
		t.Fatalf("%+v", d.Engines[1])
	}
	if e := parseDatabases("##mysql\nversion=\nconns=\n").Engines; len(e) != 1 || e[0].Reachable {
		t.Fatalf("a database that did not answer is not reachable: %+v", e)
	}
}

func TestParsePortsRules(t *testing.T) {
	p := parsePorts(`##ss
LISTEN 0 128 0.0.0.0:22 0.0.0.0:*
##ufw
Status: active

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere
80                         ALLOW       Anywhere
8080/tcp                   ALLOW       10.0.0.5
22/tcp (v6)                ALLOW       Anywhere (v6)
`)
	if len(p.Rules) != 2 || p.Rules[0] != (ServerRule{Port: 22, Proto: "tcp"}) || p.Rules[1] != (ServerRule{Port: 80}) {
		t.Fatalf("%+v", p.Rules)
	}
}
