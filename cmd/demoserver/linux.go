package main

import (
	"fmt"
	"strings"
	"time"
)

// linuxReply answers the commands of Panel with the output of a made-up Ubuntu server, so the screenshots of the user guide
// can show the dashboard without a real machine. Only what the screenshots need is answered; any other command prints nothing.
// Times are taken from the clock, so "last night" is always last night.
func linuxReply(cmd string) (string, string, int) {
	switch {
	case strings.Contains(cmd, "date '+"):
		return time.Now().Format("2006-01-02T15:04:05-0700|MST") + "\n", "", 0
	case strings.Contains(cmd, "echo '##cpu'"):
		return overviewOut, "", 0
	case strings.Contains(cmd, "systemctl list-units --type=service"):
		return servicesOut, "", 0
	case strings.Contains(cmd, "echo '##passwd'"):
		return usersOut, "", 0
	case strings.Contains(cmd, "echo '##access'"):
		return incidentOut(time.Now()), "", 0
	case strings.Contains(cmd, "ps -eo"):
		return processesOut, "", 0
	}
	return "", "", 0
}

const overviewOut = `##cpu
cpu  120000 0 50000 800000 5000 0 0 0 0 0
cpu  120400 0 50200 800300 5000 0 0 0 0 0
##mem
MemTotal:       16384000 kB
MemAvailable:    6200000 kB
SwapTotal:       2097152 kB
SwapFree:        1900000 kB
##disk
/dev/sda1 102400000 87040000 15360000 85% /
##load
0.84 0.71 0.66 1/200 1234
##up
3542400.55 1000.00
##os
Ubuntu 24.04.1 LTS
6.8.0-45-generic
4
##uid
1000
##net
Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 5000 10 0 0 0 0 0 0 5000 10 0 0 0 0 0 0
  eth0: 900000000 100000 0 0 0 0 0 0 400000000 90000 0 0 0 0 0 0
`

const servicesOut = `##units
nginx.service loaded active running A high performance web server
ssh.service loaded active running OpenBSD Secure Shell server
docker.service loaded active running Docker Application Container Engine
cron.service loaded active running Regular background program processing daemon
fail2ban.service loaded active running Fail2Ban Service
postgresql@16-main.service loaded active running PostgreSQL Cluster 16-main
backup-sync.service loaded failed failed Nightly backup to object storage
##files
nginx.service enabled enabled
ssh.service enabled enabled
docker.service enabled enabled
cron.service enabled enabled
fail2ban.service enabled enabled
postgresql@16-main.service enabled enabled
backup-sync.service enabled enabled
`

const usersOut = `##passwd
root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
ubuntu:x:1000:1000:Ubuntu:/home/ubuntu:/bin/bash
deploy:x:1001:1001:Deploy:/home/deploy:/bin/bash
mai:x:1002:1002:Mai:/home/mai:/bin/bash
contractor:x:1003:1003:Contractor:/home/contractor:/bin/bash
##groups
sudo:x:27:ubuntu,mai
##keys
root 1
ubuntu 2
deploy 1
mai 3
contractor 1
##locked
contractor L 2026-10-01 0 99999 7 -1
##who
ubuntu   pts/0        2026-11-09 08:41 (203.0.113.7)
##last
root                                       **Never logged in**
ubuntu           pts/0    203.0.113.7      Mon Nov  9 08:41:12 +0700 2026
deploy           pts/1    198.51.100.20    Sun Nov  8 22:03:40 +0700 2026
mai              pts/2    203.0.113.55     Fri Nov  6 17:20:05 +0700 2026
contractor       pts/3    192.0.2.44       Wed Sep 30 11:02:30 +0700 2026
##me
ubuntu
`

const processesOut = ` 2210 app 38.5 12.4 node server.js --port 3000
 3011 redis 1.3 2.6 redis-server *:6379
  812 www-data 4.2 1.1 nginx: worker process
 1450 postgres 3.0 5.2 postgres: 16/main: writer
  901 root 0.5 0.8 /usr/bin/dockerd -H fd://
    1 root 0.0 0.1 /sbin/init splash
`

func incidentOut(now time.Time) string {
	at := func(ago time.Duration) string { return now.Add(-ago).Format("2006-01-02T15:04:05-0700") }
	minute := func(ago time.Duration) string { return now.Add(-ago).Format("2006-01-02T15:04-0700") }
	var b strings.Builder
	b.WriteString("##access\n##persistent\nyes\n##oldest\n" + at(70*time.Hour) + " web-01 kernel: Linux version 6.8.0-45-generic\n")
	b.WriteString("##events\n")
	fmt.Fprintf(&b, "%s web-01 systemd[1]: backup-sync.service: Failed with result 'exit-code'.\n", at(5*time.Hour+10*time.Minute))
	fmt.Fprintf(&b, "%s web-01 systemd[1]: nginx.service: Scheduled restart job, restart counter is at 2.\n", at(5*time.Hour+4*time.Minute))
	fmt.Fprintf(&b, "%s web-01 kernel: Out of memory: Killed process 2210 (node) total-vm:4096000kB\n", at(5*time.Hour+3*time.Minute))
	fmt.Fprintf(&b, "%s web-01 systemd-logind[640]: The system will reboot now!\n", at(4*time.Hour+30*time.Minute))
	fmt.Fprintf(&b, "%s web-01 kernel: Linux version 6.8.0-45-generic\n", at(4*time.Hour+28*time.Minute))
	b.WriteString("##errors\n")
	for i, n := range []int{3, 11, 24, 17, 5, 2} {
		fmt.Fprintf(&b, "%d %s\n", n, minute(time.Duration(5*60+10-i)*time.Minute))
	}
	b.WriteString("##docker\npresent\n")
	fmt.Fprintf(&b, "/web|2|false|0|true|%s|0001-01-01T00:00:00Z\n", now.Add(-4*time.Hour-20*time.Minute).UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(&b, "/worker|0|true|137|false|%s|%s\n", now.Add(-6*time.Hour).UTC().Format(time.RFC3339Nano), now.Add(-5*time.Hour-3*time.Minute).UTC().Format(time.RFC3339Nano))
	return b.String()
}
