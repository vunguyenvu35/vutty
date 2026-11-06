package usecase

import (
	"os/exec"
	"strings"
	"testing"

	"vutty/internal/domain"
)

func TestParseCertsSortsBySoonestAndReadsTheNamesAndDays(t *testing.T) {
	out := `@@@found
@@@cert app.example.com
notAfter=Jan  5 12:00:00 2027 GMT
X509v3 Subject Alternative Name:
    DNS:app.example.com, DNS:www.app.example.com
@@@cert blog.example.com
notAfter=Nov 14 00:00:00 2026 GMT
X509v3 Subject Alternative Name:
    DNS:blog.example.com
@@@cert broken
openssl: nothing useful
@@@now
1794096000
`
	// 1794096000 = 2026-11-08 00:00:00 UTC
	res := parseCerts(out)
	if !res.Found || len(res.Certs) != 2 {
		t.Fatalf("%+v", res)
	}
	b, a := res.Certs[0], res.Certs[1]
	if b.Name != "blog.example.com" || b.DaysLeft != 6 || len(b.Domains) != 1 {
		t.Fatalf("%+v", b)
	}
	if a.Name != "app.example.com" || a.DaysLeft != 58 || len(a.Domains) != 2 || a.Domains[1] != "www.app.example.com" {
		t.Fatalf("%+v", a)
	}
}

func TestParseCertsNotFoundAndExpired(t *testing.T) {
	res := parseCerts("")
	if res.Found || res.Certs == nil || len(res.Certs) != 0 {
		t.Fatalf("%+v", res)
	}
	res = parseCerts("@@@found\n@@@cert old\nnotAfter=Jan  1 00:00:00 2026 GMT\n@@@now\n1794096000\n")
	if len(res.Certs) != 1 || res.Certs[0].DaysLeft >= 0 {
		t.Fatalf("an expired certificate has negative days: %+v", res)
	}
}

const sampleNginx = `nginx: the configuration file /etc/nginx/nginx.conf syntax is ok
# configuration file /etc/nginx/nginx.conf:
user www-data;
events { worker_connections 768; }
http {
    ## a comment that starts with two hashes
    map $http_upgrade $connection_upgrade { default upgrade; '' close; }
    server {
        listen 80 default_server;
        listen [::]:80 default_server;
        server_name _;
        return 301 https://$host$request_uri;
    }
    # configuration file /etc/nginx/sites-enabled/app:
    server {
        listen 443 ssl; # tls
        server_name app.example.com www.app.example.com;
        root /var/www/app;
        location / { try_files $uri @node; }
        location @node {
            proxy_pass http://127.0.0.1:3000;
            proxy_set_header Host ${host};
        }
        location /api/ { if ($request_method = POST) { return 200 "ok"; } proxy_pass http://127.0.0.1:3000; }
    }
}
stream {
    server { listen 5432; proxy_pass db:5432; }
}
`

func TestParseNginxListsTheServerBlocksOfTheHTTPBlock(t *testing.T) {
	sites := parseNginx(sampleNginx)
	if len(sites) != 2 {
		t.Fatalf("%+v", sites)
	}
	s0, s1 := sites[0], sites[1]
	if len(s0.Listen) != 2 || s0.Names[0] != "_" || len(s0.Targets) != 1 || s0.Targets[0] != "return 301 https://$host$request_uri" {
		t.Fatalf("%+v", s0)
	}
	if len(s1.Names) != 2 || s1.Names[1] != "www.app.example.com" || len(s1.Listen) != 1 || s1.Listen[0] != "443" {
		t.Fatalf("%+v", s1)
	}
	if len(s1.Targets) != 2 || s1.Targets[0] != "root /var/www/app" || s1.Targets[1] != "proxy_pass http://127.0.0.1:3000" { // the same proxy_pass twice shows once; "return 200" is left out
		t.Fatalf("%+v", s1.Targets)
	}
}

func TestParseCaddyfile(t *testing.T) {
	conf := `{
	email admin@example.com
}
(logging) {
	log
}
app.example.com, www.example.com {
	encode gzip
	reverse_proxy localhost:3000 {
		health_uri /up
	}
}
http://static.example.com:8080 {
	root * /srv/static
	file_server
}
`
	sites := parseCaddyfile(conf)
	if len(sites) != 2 {
		t.Fatalf("%+v", sites)
	}
	if len(sites[0].Names) != 2 || sites[0].Names[0] != "app.example.com" || len(sites[0].Targets) != 1 || sites[0].Targets[0] != "reverse_proxy localhost:3000 {"[:len("reverse_proxy localhost:3000")] {
		t.Fatalf("%+v", sites[0])
	}
	if sites[1].Names[0] != "static.example.com" || len(sites[1].Listen) != 1 || sites[1].Listen[0] != "8080" || len(sites[1].Targets) != 2 {
		t.Fatalf("%+v", sites[1])
	}
}

func TestParseWebReadsKindVersionAndRunning(t *testing.T) {
	w := parseWeb("@@@kind\nnginx\n@@@version\nnginx version: nginx/1.24.0 (Ubuntu)\n@@@running\nactive\n@@@conf\n" + sampleNginx)
	if w.Kind != "nginx" || w.Version != "1.24.0" || !w.Running || len(w.Sites) != 2 {
		t.Fatalf("%+v", w)
	}
	c := parseWeb("@@@kind\ncaddy\n@@@version\nv2.7.6 h1:abc=\n@@@running\ninactive\n@@@conf\nexample.com {\n respond \"hi\"\n}\n")
	if c.Kind != "caddy" || c.Version != "2.7.6" || c.Running || len(c.Sites) != 1 {
		t.Fatalf("%+v", c)
	}
	if none := parseWeb(""); none.Kind != "" || none.Sites == nil {
		t.Fatalf("%+v", none)
	}
}

func TestParseTestResult(t *testing.T) {
	ok := parseTestResult("0\nnginx: configuration file /etc/nginx/nginx.conf test is successful\n")
	bad := parseTestResult("1\nnginx: [emerg] unknown directive \"foo\" in /etc/nginx/nginx.conf:3\n")
	if !ok.OK || bad.OK || !strings.Contains(bad.Output, "unknown directive") {
		t.Fatalf("%+v %+v", ok, bad)
	}
}

func TestComposeProjectFromLabels(t *testing.T) {
	labels := "com.docker.compose.config-hash=abc,com.docker.compose.project=shop,com.docker.compose.service=api"
	if composeProject(labels) != "shop" || composeProject("maintainer=me") != "" || composeProject("com.docker.compose.project=Bad Name") != "" {
		t.Fatal("project")
	}
	c := parseContainers(`{"ID":"a1","Names":"shop-api-1","Image":"api:1","State":"running","Status":"Up","Ports":"","Labels":"` + labels + `"}` + "\n" + `{"ID":"b2","Names":"redis","Image":"redis","State":"running","Status":"Up","Ports":"","Labels":""}`)
	if len(c.Containers) != 2 || c.Containers[0].Project+c.Containers[1].Project != "shop" {
		t.Fatalf("%+v", c)
	}
}

func TestParseUpdatesApt(t *testing.T) {
	out := `@@@mgr
apt
@@@list
Listing...
nginx/jammy-updates,jammy-security 1.18.0-6ubuntu14.5 amd64 [upgradable from: 1.18.0-6ubuntu14.4]
vim/jammy-updates 2:8.2.3995-1ubuntu2.20 amd64 [upgradable from: 2:8.2.3995-1ubuntu2.19]
openssl/jammy-updates,jammy-security 3.0.2-0ubuntu1.18 amd64 [upgradable from: 3.0.2-0ubuntu1.17]
@@@reboot
yes
@@@stamp
1794000000
@@@now
1794003600
`
	u := parseUpdates(out)
	if u.Manager != "apt" || u.Count != 3 || u.Security != 2 || !u.RebootRequired || u.ListsAgeSec != 3600 {
		t.Fatalf("%+v", u)
	}
	if !u.Packages[0].Security || !u.Packages[1].Security || u.Packages[2].Name != "vim" || u.Packages[2].Version != "2:8.2.3995-1ubuntu2.20" {
		t.Fatalf("security first: %+v", u.Packages)
	}
}

func TestParseUpdatesDnfAndNone(t *testing.T) {
	out := `@@@mgr
dnf
@@@list

kernel.x86_64                      5.14.0-427.el9    baseos
openssl-libs.x86_64                1:3.0.7-25.el9    baseos
python3.11.x86_64                  3.11.7-1.el9      appstream

Obsoleting Packages
foo.x86_64 1.0 repo
@@@sec
RHSA-2024:1234 Important/Sec. openssl-libs-1:3.0.7-25.el9.x86_64
@@@reboot
yes
`
	u := parseUpdates(out)
	if u.Manager != "dnf" || u.Count != 3 || u.Security != 1 || u.Packages[0].Name != "openssl-libs" || !u.RebootRequired || u.ListsAgeSec != -1 {
		t.Fatalf("%+v", u)
	}
	if none := parseUpdates(""); none.Manager != "" || none.Count != 0 || none.ListsAgeSec != -1 || none.Packages == nil {
		t.Fatalf("%+v", none)
	}
}

func TestParseDockerSize(t *testing.T) {
	for in, want := range map[string]uint64{"0B": 0, "512MB": 512e6, "1.2GB (45%)": 1200000000, "3.5kB": 3500, "2GiB": 2 << 30, "": 0, "n/a": 0, "1.5TB": 1500000000000} {
		if got := parseDockerSize(in); got != want {
			t.Errorf("%q: %d, want %d", in, got, want)
		}
	}
}

func TestParseCleanup(t *testing.T) {
	out := `@@@df
Filesystem     Type     1K-blocks     Used Available Capacity Mounted on
/dev/vda1      ext4      51200000 20480000  30720000      40% /
tmpfs          tmpfs      1000000        0   1000000       0% /dev/shm
/dev/vda15     vfat        106858     6186    100673       6% /boot/efi
/dev/vda1      ext4      51200000 20480000  30720000      40% /var/lib/docker/overlay2/x
/dev/loop0     squashfs      64000    64000         0     100% /snap/core/1
@@@docker
@@@dockerdf
{"Active":"3","Reclaimable":"1.2GB (45%)","Size":"2.6GB","TotalCount":"5","Type":"Images"}
{"Active":"0","Reclaimable":"0B","Size":"0B","TotalCount":"0","Type":"Containers"}
{"Active":"2","Reclaimable":"800MB (30%)","Size":"2.6GB","TotalCount":"4","Type":"Local Volumes"}
{"Active":"0","Reclaimable":"300MB","Size":"300MB","TotalCount":"9","Type":"Build Cache"}
@@@dangling
100MB
50MB
@@@journal
Archived and active journals take up 1.5G in the file system.
@@@apt
@@@aptsize
2048	/var/cache/apt
`
	c := parseCleanup(out)
	if len(c.Disks) != 2 || c.Disks[0].Mount != "/" || c.Disks[0].Total != 51200000*1024 || c.Disks[1].Mount != "/boot/efi" {
		t.Fatalf("%+v", c.Disks)
	}
	size := map[string]uint64{}
	for _, it := range c.Items {
		size[it.ID] = it.Size
	}
	if len(c.Items) != 5 || size["docker-dangling"] != 450e6 || size["docker-images"] != 1200000000 || size["docker-volumes"] != 800e6 || size["journal"] != 1610612736 || size["apt-cache"] != 2048*1024 {
		t.Fatalf("%+v", c.Items)
	}
	if none := parseCleanup(""); none.Items == nil || none.Disks == nil || len(none.Items) != 0 {
		t.Fatalf("%+v", none)
	}
}

func TestParseFolders(t *testing.T) {
	out := "4\t/var/mail\n9000\t/var/lib\n12000\t/var\n300\t/var/log\n"
	f := parseFolders(out, "/var/")
	if len(f) != 3 || f[0].Path != "/var/lib" || f[0].Size != 9000*1024 || f[2].Path != "/var/mail" {
		t.Fatalf("%+v", f)
	}
	if root := parseFolders("5\t/etc\n9\t/\n", "/"); len(root) != 1 || root[0].Path != "/etc" {
		t.Fatalf("%+v", root)
	}
}

func TestParseSecurity(t *testing.T) {
	out := `@@@f2binstalled
@@@f2brunning
@@@jail sshd
Status for the jail: sshd
|- Filter
|  |- Currently failed:	1
|  |- Total failed:	123
|  ` + "`" + `- File list:	/var/log/auth.log
` + "`" + `- Actions
   |- Currently banned:	2
   |- Total banned:	10
   ` + "`" + `- Banned IP list:	203.0.113.7 2001:db8::5 not-an-ip
@@@jail bad name!
Status for the jail: x
@@@ssh
port 2222
permitrootlogin without-password
passwordauthentication no
port 22
@@@known
1
@@@auth
sshd[1]: Failed password for root from 203.0.113.7 port 4000 ssh2
sshd[1]: Failed password for root from 203.0.113.7 port 4001 ssh2
sshd[1]: Invalid user admin from 198.51.100.9 port 5000
sshd[1]: Failed password for invalid user admin from 198.51.100.9 port 5000 ssh2
sshd[1]: Failed password for bob from 2001:db8::99 port 6000 ssh2
sshd[1]: Failed password for bob from garbage port 1
`
	s := parseSecurity(out)
	if !s.Fail2ban.Installed || !s.Fail2ban.Running || len(s.Fail2ban.Jails) != 1 {
		t.Fatalf("%+v", s.Fail2ban)
	}
	j := s.Fail2ban.Jails[0]
	if j.Name != "sshd" || j.Banned != 2 || j.TotalBanned != 10 || len(j.IPs) != 2 || j.IPs[1] != "2001:db8::5" {
		t.Fatalf("%+v", j)
	}
	if !s.SSH.Known || s.SSH.Port != 2222 || s.SSH.RootLogin != "without-password" || s.SSH.PasswordAuth != "no" {
		t.Fatalf("%+v", s.SSH)
	}
	if !s.Failed.Known || s.Failed.Total != 4 || len(s.Failed.Top) != 3 || s.Failed.Top[0].IP != "203.0.113.7" || s.Failed.Top[0].Count != 2 { // the repeated "invalid user" line and the garbage address are not counted
		t.Fatalf("%+v", s.Failed)
	}
	if empty := parseSecurity(""); empty.Fail2ban.Installed || empty.SSH.Known || empty.Failed.Known || empty.Fail2ban.Jails == nil || empty.Failed.Top == nil {
		t.Fatalf("%+v", empty)
	}
}

func TestParseSchedule(t *testing.T) {
	out := `@@@timer backup.timer
Unit=backup.service
Description=Nightly backup
NextElapseUSecRealtime=Thu 2026-11-05 03:00:00 UTC
LastTriggerUSec=Wed 2026-11-04 03:00:00 UTC
@@@result
Result=exit-code
@@@timer certbot.timer
Unit=certbot.service
Description=Certbot
NextElapseUSecRealtime=Thu 2026-11-05 11:00:00 UTC
LastTriggerUSec=n/a
@@@result
Result=success
@@@cron crontab
# m h dom mon dow command
MAILTO=me@example.com
*/5 * * * * /home/ubuntu/ping.sh --quiet
@reboot /home/ubuntu/start.sh
@@@cron root
@@@cron /etc/crontab
SHELL=/bin/sh
17 *	* * *	root    cd / && run-parts --report /etc/cron.hourly
@@@cron /etc/cron.d/certbot
0 */12 * * * root test -x /usr/bin/certbot && certbot -q renew
`
	s := parseSchedule(out)
	if len(s.Timers) != 2 || s.Timers[0].Unit != "backup.service" || s.Timers[0].Result != "exit-code" || s.Timers[0].Last == "" || s.Timers[0].Description != "Nightly backup" {
		t.Fatalf("%+v", s.Timers)
	}
	if s.Timers[1].Last != "" || s.Timers[1].Result != "" || s.Timers[1].Next == "" { // it never ran: no result to report
		t.Fatalf("%+v", s.Timers[1])
	}
	if len(s.Cron) != 4 {
		t.Fatalf("%+v", s.Cron)
	}
	if c := s.Cron[0]; c.Source != "crontab" || c.Schedule != "*/5 * * * *" || c.Command != "/home/ubuntu/ping.sh --quiet" || c.User != "" {
		t.Fatalf("%+v", c)
	}
	if c := s.Cron[1]; c.Schedule != "@reboot" || c.Command != "/home/ubuntu/start.sh" {
		t.Fatalf("%+v", c)
	}
	if c := s.Cron[2]; c.Source != "/etc/crontab" || c.User != "root" || c.Command != "cd / && run-parts --report /etc/cron.hourly" {
		t.Fatalf("%+v", c)
	}
	if c := s.Cron[3]; c.Source != "/etc/cron.d/certbot" || c.Schedule != "0 */12 * * *" || c.User != "root" {
		t.Fatalf("%+v", c)
	}
}

func TestParseBackup(t *testing.T) {
	b := parseBackup("@@@now\n1794096000\n@@@newest\n1794009600.1234567890 52428800 db dumps/2026-11-07 full.sql.gz\n")
	if !b.Found || b.File != "db dumps/2026-11-07 full.sql.gz" || b.Size != 52428800 || b.MTime != 1794009600 || b.Now != 1794096000 {
		t.Fatalf("%+v", b)
	}
	if none := parseBackup("@@@now\n5\n@@@newest\n\n"); none.Found || none.Now != 5 {
		t.Fatalf("%+v", none)
	}
}

func TestParseFleet(t *testing.T) {
	o, n := parseFleet(sampleOverview + "##failed\n2\n")
	if o.Cores != 4 || n != 2 {
		t.Fatalf("%+v %d", o, n)
	}
}

// What the screen sends is checked before any command is built: there is no session here, so a request that got past the
// check would fail with "the connection to the server is closed" instead.
func TestServerToolsRefuseWhatIsNotCheckedBeforeAnyCommand(t *testing.T) {
	s := NewServerService(nil)
	cases := map[string]error{
		"stack name":    s.StackAction("x", "Shop; rm -rf /", "start"),
		"stack caps":    s.StackAction("x", "Shop", "start"),
		"stack action":  s.StackAction("x", "shop", "destroy"),
		"web kind":      func() error { _, e := s.WebReload("x", "apache"); return e }(),
		"web test kind": func() error { _, e := s.WebTest("x", "nginx; id"); return e }(),
		"cleanup id":    s.CleanupRun("x", "docker-volumes"),
		"cleanup other": s.CleanupRun("x", "journal; rm -rf /"),
		"unban jail":    s.Unban("x", "sshd; id", "1.2.3.4"),
		"unban ip":      s.Unban("x", "sshd", "1.2.3.4; id"),
		"unban ip 2":    s.Unban("x", "sshd", "$(id)"),
		"du relative":   func() error { _, e := s.DiskUsage("x", "var/log"); return e }(),
		"du dotdot":     func() error { _, e := s.DiskUsage("x", "/var/../etc"); return e }(),
		"du quote":      func() error { _, e := s.DiskUsage("x", `/var"; id; "`); return e }(),
		"du space":      func() error { _, e := s.DiskUsage("x", "/var/my logs"); return e }(),
		"du empty":      func() error { _, e := s.DiskUsage("x", ""); return e }(),
		"backup dotdot": func() error { _, e := s.Backup("x", "/srv/../etc"); return e }(),
		"backup dollar": func() error { _, e := s.Backup("x", "/srv/$HOME"); return e }(),
		"logs stack":    func() error { _, e := s.Logs("x", "stack", "Shop; id"); return e }(),
		"logs web":      func() error { _, e := s.Logs("x", "web", "apache"); return e }(),
		"logs kind":     func() error { _, e := s.Logs("x", "file", "syslog"); return e }(),
		"logs lines":    func() error { _, e := s.LogsFor("x", "", "", 7, ""); return e }(),
		"logs since":    func() error { _, e := s.LogsFor("x", "", "", 100, "1h; id"); return e }(),
	}
	for name, err := range cases {
		if err == nil || strings.Contains(err.Error(), "connection to the server is closed") {
			t.Errorf("%s was not refused before a command: %v", name, err)
		}
	}
	// and what is fine gets as far as the session
	for name, err := range map[string]error{
		"stack": s.StackAction("x", "shop-1", "update"),
		"unban": s.Unban("x", "sshd", "2001:db8::1"),
		"clean": s.CleanupRun("x", "docker-dangling"),
	} {
		if err == nil || !strings.Contains(err.Error(), "connection to the server is closed") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestValidServerPath(t *testing.T) {
	for p, want := range map[string]bool{"/": true, "/var/backups": true, "/srv/db-dumps_1.2/": true, "var": false, "/a/../b": false, "/a b": false, "/a;b": false, "/a`b": false, "": false, "/..": false} {
		if domain.ValidServerPath(p) != want {
			t.Errorf("%q: want %v", p, want)
		}
	}
	h := domain.Host{Label: "x", Hostname: "h", Port: 22, BackupPath: "/srv/$(id)"}
	if err := domain.NormalizeHost(&h); err == nil {
		t.Error("a backup folder with $( passed the host check")
	}
}

// The scripts are shell text: a typo would only show on a server. sh -n reads them without running anything.
func TestServerToolScriptsAreValidShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	web, _ := webScriptFor("nginx", true)
	webCaddy, _ := webScriptFor("caddy", false)
	scripts := map[string]string{
		"certs": asRoot + certsScript, "web": asRoot + webScript, "nginx reload": web, "caddy test": webCaddy,
		"compose":  asRoot + composeScript("shop", "pull", "up -d"),
		"updates":  updatesScript,
		"cleanup":  asRoot + cleanupScript,
		"security": asRoot + securityScript, "schedule": asRoot + scheduleScript, "fleet": fleetScript,
	}
	for _, c := range cleanups {
		scripts["clean "+c] = asRoot + c
	}
	for name, script := range scripts {
		if out, err := exec.Command(sh, "-n", "-c", script).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
	}
}

func TestDockerPullAndStackUpDownChecks(t *testing.T) {
	s := &ServerService{sessions: map[string]*serverSession{}}
	for _, img := range []string{"", "Nginx", "nginx; reboot", "-v", "a b"} {
		if _, err := s.DockerPull("x", img); err == nil || err.Error() == "the connection to the server is closed: reconnect" {
			t.Fatalf("%q should be refused before the server is asked: %v", img, err)
		}
	}
	for _, img := range []string{"nginx", "nginx:1.27", "ghcr.io/org/app:v1", "registry:5000/a/b@sha256:abc"} {
		if !imageRefRe.MatchString(img) {
			t.Fatalf("%q should pass", img)
		}
	}
	if _, ok := stackActions["up"]; !ok {
		t.Fatal("up")
	}
	if _, ok := stackActions["down"]; !ok {
		t.Fatal("down")
	}
}

func TestParseFleetCertDays(t *testing.T) {
	out := "##certs\nnotAfter=Jan 12 00:00:00 2027 GMT\nnotAfter=Jan  2 00:00:00 2027 GMT\n##now\n1798000000\n"
	// 1798000000 = 2026-12-23 04:26:40 UTC; the soonest end is 2027-01-02 00:00:00 UTC, 9 days and a few hours later
	if d := parseFleetCertDays(out); d == nil || *d != 9 {
		t.Fatalf("%v", d)
	}
	if d := parseFleetCertDays("##certs\n##now\n1798000000\n"); d != nil {
		t.Fatalf("%v", *d)
	}
	if d := parseFleetCertDays("##certs\nnotAfter=Dec 20 00:00:00 2026 GMT\n##now\n1798000000\n"); d == nil || *d != -4 {
		t.Fatalf("%v", d)
	}
}

func TestClipNotificationText(t *testing.T) {
	if got := clip("a\nb\r\nc", 80); got != "a b  c" {
		t.Fatalf("%q", got)
	}
	if got := clip("abcdef", 4); got != "abc…" {
		t.Fatalf("%q", got)
	}
	if err := (&ServerService{}).Notify("", "x"); err == nil {
		t.Fatal("title")
	}
}

func TestFleetActionRefusesUnknownAction(t *testing.T) {
	if _, err := (&ServerService{}).FleetAction("h", "reboot"); err == nil || err.Error() != "unknown action" {
		t.Fatal(err)
	}
	if lastLine("a\nb\n\n") != "b" {
		t.Fatal("lastLine")
	}
}
