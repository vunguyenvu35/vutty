package usecase

import (
	"encoding/json"
	"math"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The scripts of the server tools print a "@@@name arg" line before each part of their output. Unlike the "##name" of the
// first screens it cannot be mistaken for the output itself: nginx files, package lists and logs are full of "##".
type markedPart struct{ name, arg, body string }

func splitMarked(out string) []markedPart {
	var parts []markedPart
	var b strings.Builder
	flush := func() {
		if len(parts) > 0 {
			parts[len(parts)-1].body = strings.TrimRight(b.String(), "\n")
		}
		b.Reset()
	}
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "@@@") {
			flush()
			name, arg, _ := strings.Cut(strings.TrimSpace(ln[3:]), " ")
			parts = append(parts, markedPart{name: name, arg: strings.TrimSpace(arg)})
			continue
		}
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	flush()
	return parts
}

// marked returns the body of the first part with that name, and whether there is one.
func marked(parts []markedPart, name string) (string, bool) {
	for _, p := range parts {
		if p.name == name {
			return p.body, true
		}
	}
	return "", false
}

func appendUnique(list []string, v string) []string {
	if v == "" {
		return list
	}
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// ---- certificates ----

// ServerCert is a Let's Encrypt certificate on the server.
type ServerCert struct {
	Name     string   `json:"name"`
	Domains  []string `json:"domains"`
	NotAfter int64    `json:"not_after"` // unix seconds
	DaysLeft int      `json:"days_left"`
}

// ServerCerts is the Certificates section: Found is false when /etc/letsencrypt is missing or the login cannot read it.
type ServerCerts struct {
	Found bool         `json:"found"`
	Certs []ServerCert `json:"certs"`
}

var sanRe = regexp.MustCompile(`DNS:([^\s,]+)`)

// parseCerts reads the output of certsScript: "@@@cert <name>" with the lines of `openssl x509 -enddate -ext subjectAltName`.
func parseCerts(out string) ServerCerts {
	parts := splitMarked(out)
	res := ServerCerts{Certs: []ServerCert{}}
	_, res.Found = marked(parts, "found")
	now := time.Now().Unix()
	if s, ok := marked(parts, "now"); ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && n > 0 {
			now = n
		}
	}
	for _, p := range parts {
		if p.name != "cert" {
			continue
		}
		c := ServerCert{Name: p.arg, Domains: []string{}}
		for _, ln := range strings.Split(p.body, "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(ln), "notAfter="); ok {
				if t, err := time.Parse("Jan _2 15:04:05 2006 GMT", strings.TrimSpace(v)); err == nil {
					c.NotAfter = t.Unix()
				}
			}
		}
		for _, m := range sanRe.FindAllStringSubmatch(p.body, -1) {
			c.Domains = appendUnique(c.Domains, m[1])
		}
		if c.NotAfter == 0 {
			continue
		}
		c.DaysLeft = int(math.Floor(float64(c.NotAfter-now) / 86400))
		res.Certs = append(res.Certs, c)
	}
	sort.SliceStable(res.Certs, func(i, j int) bool { return res.Certs[i].NotAfter < res.Certs[j].NotAfter })
	return res
}

// ---- web server ----

// ServerSite is a server block of nginx or a site of a Caddyfile.
type ServerSite struct {
	Names   []string `json:"names"`
	Listen  []string `json:"listen"`
	Targets []string `json:"targets"` // "proxy_pass http://127.0.0.1:3000", "root /var/www/app", "reverse_proxy localhost:3000"…
}

// ServerWeb is the Web section: Kind is "" when neither nginx nor Caddy is installed.
type ServerWeb struct {
	Kind    string       `json:"kind"`
	Version string       `json:"version"`
	Running bool         `json:"running"`
	Sites   []ServerSite `json:"sites"`
}

// ServerWebResult is what nginx -t (caddy validate) printed. For a reload OK means that the test passed and the server was reloaded.
type ServerWebResult struct {
	OK     bool   `json:"ok"`
	Output string `json:"output"`
}

const maxWebSites = 100

var nginxVersionRe = regexp.MustCompile(`nginx/([0-9][^\s]*)`)

// parseWeb reads the output of webScript.
func parseWeb(out string) ServerWeb {
	parts := splitMarked(out)
	res := ServerWeb{Sites: []ServerSite{}}
	kind, _ := marked(parts, "kind")
	res.Kind = strings.TrimSpace(kind)
	if res.Kind != "nginx" && res.Kind != "caddy" {
		res.Kind = ""
		return res
	}
	v, _ := marked(parts, "version")
	if res.Kind == "nginx" {
		if m := nginxVersionRe.FindStringSubmatch(v); m != nil {
			res.Version = m[1]
		}
	} else if f := strings.Fields(v); len(f) > 0 {
		res.Version = strings.TrimPrefix(f[0], "v")
	}
	r, _ := marked(parts, "running")
	res.Running = strings.TrimSpace(strings.SplitN(r, "\n", 2)[0]) == "active"
	conf, _ := marked(parts, "conf")
	if res.Kind == "nginx" {
		res.Sites = parseNginx(conf)
	} else {
		res.Sites = parseCaddyfile(conf)
	}
	if len(res.Sites) > maxWebSites {
		res.Sites = res.Sites[:maxWebSites]
	}
	return res
}

type nginxToken struct {
	s   string
	sep bool // one of { } ;
}

// nginxTokens splits a configuration into words and the three separators, dropping comments and the quotes around words.
func nginxTokens(conf string) []nginxToken {
	var toks []nginxToken
	var b strings.Builder
	have := false // a word is being read (it may be an empty quoted string)
	flush := func() {
		if have && b.Len() > 0 {
			toks = append(toks, nginxToken{s: b.String()})
		}
		b.Reset()
		have = false
	}
	for i := 0; i < len(conf); i++ {
		c := conf[i]
		switch {
		case c == '#' && !have:
			for i < len(conf) && conf[i] != '\n' {
				i++
			}
		case c == '"' || c == '\'':
			have = true
			for i++; i < len(conf) && conf[i] != c; i++ {
				if conf[i] == '\\' && i+1 < len(conf) {
					i++
				}
				b.WriteByte(conf[i])
			}
		case c == '$' && i+1 < len(conf) && conf[i+1] == '{': // ${name}: the braces belong to the word
			have = true
			for ; i < len(conf) && conf[i] != '}'; i++ {
				b.WriteByte(conf[i])
			}
			b.WriteByte('}')
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		case c == '{' || c == '}' || c == ';':
			flush()
			toks = append(toks, nginxToken{s: string(c), sep: true})
		default:
			have = true
			b.WriteByte(c)
		}
	}
	flush()
	return toks
}

// parseNginx lists the server blocks of the output of `nginx -T` that are in the http block (the stream block is left out).
func parseNginx(conf string) []ServerSite {
	var sites []ServerSite
	var stack []string
	var words []string
	var cur *ServerSite
	depth := 0
	for _, t := range nginxTokens(conf) {
		switch {
		case !t.sep:
			words = append(words, t.s)
		case t.s == ";":
			if cur != nil {
				nginxDirective(cur, words)
			}
			words = nil
		case t.s == "{":
			name, parent := "", ""
			if len(words) > 0 {
				name = words[0]
			}
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			stack = append(stack, name)
			if name == "server" && cur == nil && (parent == "http" || parent == "") {
				cur = &ServerSite{Names: []string{}, Listen: []string{}, Targets: []string{}}
				depth = len(stack)
			}
			words = nil
		default: // }
			if len(stack) > 0 {
				if cur != nil && len(stack) == depth {
					sites = append(sites, *cur)
					cur, depth = nil, 0
				}
				stack = stack[:len(stack)-1]
			}
			words = nil
		}
	}
	return sites
}

func nginxDirective(s *ServerSite, w []string) {
	if len(w) < 2 {
		return
	}
	switch w[0] {
	case "server_name":
		for _, n := range w[1:] {
			s.Names = appendUnique(s.Names, n)
		}
	case "listen":
		s.Listen = appendUnique(s.Listen, w[1])
	case "proxy_pass", "fastcgi_pass", "uwsgi_pass", "scgi_pass", "grpc_pass", "root":
		s.Targets = appendUnique(s.Targets, w[0]+" "+w[1])
	case "return": // only a redirect says where the site goes: "return 404" and "return 200 ok" are noise
		if len(w) >= 3 && strings.HasPrefix(w[1], "3") {
			s.Targets = appendUnique(s.Targets, strings.Join(w[:3], " "))
		}
	}
}

// parseCaddyfile lists the sites of a Caddyfile: the blocks at the top level, except the global options and the snippets.
func parseCaddyfile(conf string) []ServerSite {
	var sites []ServerSite
	var cur *ServerSite
	depth := 0
	for _, raw := range strings.Split(conf, "\n") {
		ln := strings.TrimSpace(raw)
		if i := strings.Index(ln, "#"); i == 0 {
			continue
		} else if i > 0 && (ln[i-1] == ' ' || ln[i-1] == '\t') {
			ln = strings.TrimSpace(ln[:i])
		}
		if ln == "" {
			continue
		}
		opens := strings.HasSuffix(ln, "{")
		switch {
		case ln == "}":
			if depth > 0 {
				depth--
			}
			if depth == 0 && cur != nil {
				sites = append(sites, *cur)
				cur = nil
			}
		case depth == 0 && opens:
			depth = 1
			head := strings.TrimSpace(strings.TrimSuffix(ln, "{"))
			if head == "" || strings.HasPrefix(head, "(") { // the global options, a snippet
				break
			}
			cur = &ServerSite{Names: []string{}, Listen: []string{}, Targets: []string{}}
			for _, a := range strings.FieldsFunc(head, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
				a = strings.TrimPrefix(strings.TrimPrefix(a, "https://"), "http://")
				host := a
				if i := strings.LastIndex(a, ":"); i >= 0 {
					if _, err := strconv.Atoi(a[i+1:]); err == nil {
						host = a[:i]
						cur.Listen = appendUnique(cur.Listen, a[i+1:])
					}
				}
				cur.Names = appendUnique(cur.Names, host)
			}
		case cur != nil:
			w := strings.Fields(strings.TrimSpace(strings.TrimSuffix(ln, "{")))
			if len(w) > 0 {
				switch w[0] {
				case "reverse_proxy", "php_fastcgi", "redir", "root", "respond":
					cur.Targets = appendUnique(cur.Targets, strings.Join(w, " "))
				case "file_server":
					cur.Targets = appendUnique(cur.Targets, "file_server")
				}
			}
			if opens {
				depth++
			}
		default:
			if opens {
				depth++
			}
		}
	}
	return sites
}

// parseTestResult reads "<exit code>\n<output>" (see webTestScript).
func parseTestResult(out string) ServerWebResult {
	first, rest, _ := strings.Cut(out, "\n")
	return ServerWebResult{OK: strings.TrimSpace(first) == "0", Output: strings.TrimSpace(rest)}
}

// ---- compose ----

// composeNameRe is the shape of a Docker Compose project name.
var composeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// composeProject reads the Compose project from the Labels of `docker ps --format '{{json .}}'` ("a=b,c=d").
func composeProject(labels string) string {
	for _, l := range strings.Split(labels, ",") {
		if v, ok := strings.CutPrefix(l, "com.docker.compose.project="); ok && composeNameRe.MatchString(v) {
			return v
		}
	}
	return ""
}

// ---- updates ----

// ServerPackage is a package with an update waiting.
type ServerPackage struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Security bool   `json:"security"`
}

// ServerUpdates is the Updates section. ListsAgeSec is how long ago the package lists were refreshed (-1 when unknown): the figures are only as fresh as that.
type ServerUpdates struct {
	Manager        string          `json:"manager"`
	Count          int             `json:"count"`
	Security       int             `json:"security"`
	RebootRequired bool            `json:"reboot_required"`
	ListsAgeSec    int64           `json:"lists_age_sec"`
	Packages       []ServerPackage `json:"packages"`
}

const maxUpdatePackages = 100

// parseUpdates reads the output of updatesScript.
func parseUpdates(out string) ServerUpdates {
	parts := splitMarked(out)
	res := ServerUpdates{ListsAgeSec: -1, Packages: []ServerPackage{}}
	mgr, _ := marked(parts, "mgr")
	switch m := strings.TrimSpace(mgr); m {
	case "apt", "dnf", "yum":
		res.Manager = m
	default:
		return res
	}
	list, _ := marked(parts, "list")
	secNames := map[string]bool{}
	if sec, ok := marked(parts, "sec"); ok {
		for _, ln := range strings.Split(sec, "\n") {
			f := strings.Fields(ln)
			if len(f) < 3 {
				continue
			}
			secNames[rpmName(f[len(f)-1])] = true // "RHSA-2024:1 Important/Sec. openssl-libs-3.0.7-24.el9.x86_64"
		}
	}
	var pk []ServerPackage
	for _, ln := range strings.Split(list, "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		if res.Manager == "apt" {
			name, _, ok := strings.Cut(f[0], "/")
			if !ok { // "Listing..." and warnings
				continue
			}
			pk = append(pk, ServerPackage{Name: name, Version: f[1], Security: strings.Contains(ln, "-security")})
			continue
		}
		if strings.HasPrefix(ln, "Obsoleting Packages") {
			break
		}
		if len(f) < 3 || !strings.Contains(f[0], ".") {
			continue
		}
		name := f[0][:strings.LastIndex(f[0], ".")]
		pk = append(pk, ServerPackage{Name: name, Version: f[1], Security: secNames[name]})
	}
	res.Count = len(pk)
	sort.SliceStable(pk, func(i, j int) bool { return pk[i].Security && !pk[j].Security })
	for _, p := range pk {
		if p.Security {
			res.Security++
		}
	}
	if len(pk) > maxUpdatePackages {
		pk = pk[:maxUpdatePackages]
	}
	if len(pk) > 0 {
		res.Packages = pk
	}
	reboot, _ := marked(parts, "reboot")
	res.RebootRequired = strings.TrimSpace(reboot) == "yes"
	stamp, _ := marked(parts, "stamp")
	now, _ := marked(parts, "now")
	st, e1 := strconv.ParseInt(strings.TrimSpace(stamp), 10, 64)
	nw, e2 := strconv.ParseInt(strings.TrimSpace(now), 10, 64)
	if e1 == nil && e2 == nil && st > 0 && nw >= st {
		res.ListsAgeSec = nw - st
	}
	return res
}

// rpmName turns "openssl-libs-3.0.7-24.el9.x86_64" into "openssl-libs".
func rpmName(nevra string) string {
	if i := strings.LastIndex(nevra, "."); i > 0 {
		nevra = nevra[:i] // the architecture
	}
	for n := 0; n < 2; n++ { // the release, then the version
		if i := strings.LastIndex(nevra, "-"); i > 0 {
			nevra = nevra[:i]
		}
	}
	return nevra
}

// ---- cleanup ----

// ServerDisk is a mounted file system.
type ServerDisk struct {
	Mount  string `json:"mount"`
	Device string `json:"device"`
	Total  uint64 `json:"total"` // bytes
	Used   uint64 `json:"used"`
}

// ServerReclaim is something that can be cleaned, and about how much it would free. ID is docker-dangling, docker-images,
// journal, apt-cache or docker-volumes (shown only: CleanupRun refuses it).
type ServerReclaim struct {
	ID   string `json:"id"`
	Size uint64 `json:"size"` // bytes, 0 when unknown
	Note string `json:"note"`
}

// ServerCleanup is the Disk and cleanup section.
type ServerCleanup struct {
	Disks []ServerDisk    `json:"disks"`
	Items []ServerReclaim `json:"items"`
}

// ServerFolder is a folder and the space it takes.
type ServerFolder struct {
	Path string `json:"path"`
	Size uint64 `json:"size"`
}

const maxFolders = 30

var skipFS = map[string]bool{"tmpfs": true, "devtmpfs": true, "devfs": true, "squashfs": true, "overlay": true, "efivarfs": true, "ramfs": true, "iso9660": true, "proc": true, "sysfs": true}

// parseDisks reads `df -PkT`: "device type 1K-blocks used available use% mount". A device that is mounted twice (btrfs subvolumes, bind mounts) shows once.
func parseDisks(body string) []ServerDisk {
	disks := []ServerDisk{}
	seen := map[string]bool{}
	for _, ln := range strings.Split(body, "\n") {
		f := strings.Fields(ln)
		if len(f) < 7 || f[0] == "Filesystem" || skipFS[f[1]] {
			continue
		}
		mount := strings.Join(f[6:], " ")
		if strings.HasPrefix(mount, "/snap") || strings.HasPrefix(mount, "/run") || strings.HasPrefix(mount, "/var/lib/docker") || seen[f[0]] {
			continue
		}
		total, e1 := strconv.ParseUint(f[2], 10, 64)
		used, e2 := strconv.ParseUint(f[3], 10, 64)
		if e1 != nil || e2 != nil || total == 0 {
			continue
		}
		seen[f[0]] = true
		disks = append(disks, ServerDisk{Mount: mount, Device: f[0], Total: total * 1024, Used: used * 1024})
	}
	return disks
}

var dockerSizeRe = regexp.MustCompile(`^([0-9]*\.?[0-9]+)\s*([A-Za-z]*)`)

// parseDockerSize reads a size as Docker prints it ("1.2GB", "512MB", "3.5kB (45%)", "0B"); kB, MB… are powers of 1000, KiB, MiB… of 1024.
func parseDockerSize(s string) uint64 {
	m := dockerSizeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToLower(m[2])
	base := 1000.0
	if strings.HasSuffix(unit, "ib") {
		base = 1024
	}
	pow := map[string]float64{"": 0, "b": 0, "k": 1, "m": 2, "g": 3, "t": 4, "p": 5}[strings.TrimSuffix(strings.TrimSuffix(unit, "ib"), "b")]
	return uint64(v * math.Pow(base, pow))
}

var journalRe = regexp.MustCompile(`take up ([0-9.]+)\s*([KMGT]?)`)

// parseJournalSize reads "Archived and active journals take up 1.2G in the file system." (units of 1024).
func parseJournalSize(s string) (uint64, bool) {
	m := journalRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return uint64(v * math.Pow(1024, float64(strings.Index(" KMGT", m[2])))), true
}

// parseCleanup reads the output of cleanupScript.
func parseCleanup(out string) ServerCleanup {
	parts := splitMarked(out)
	res := ServerCleanup{Disks: []ServerDisk{}, Items: []ServerReclaim{}}
	df, _ := marked(parts, "df")
	res.Disks = parseDisks(df)
	if _, ok := marked(parts, "docker"); ok {
		rec := map[string]uint64{}
		rows, _ := marked(parts, "dockerdf")
		for _, ln := range strings.Split(rows, "\n") {
			var j struct{ Type, Reclaimable string }
			if strings.HasPrefix(strings.TrimSpace(ln), "{") && json.Unmarshal([]byte(ln), &j) == nil {
				rec[j.Type] = parseDockerSize(j.Reclaimable)
			}
		}
		var dangling uint64
		dl, _ := marked(parts, "dangling")
		for _, ln := range strings.Split(dl, "\n") {
			dangling += parseDockerSize(ln)
		}
		res.Items = append(res.Items,
			ServerReclaim{ID: "docker-dangling", Size: dangling + rec["Build Cache"], Note: "Untagged images and the build cache"},
			ServerReclaim{ID: "docker-images", Size: rec["Images"], Note: "Every image that no container uses; they are pulled again when needed"},
			ServerReclaim{ID: "docker-volumes", Size: rec["Local Volumes"], Note: "Volumes that no container uses. They can hold data, so they are never removed from here"})
	}
	if j, ok := marked(parts, "journal"); ok {
		if n, ok := parseJournalSize(j); ok {
			res.Items = append(res.Items, ServerReclaim{ID: "journal", Size: n, Note: "The system log, cut down to 200 MB"})
		}
	}
	if _, ok := marked(parts, "apt"); ok {
		var n uint64
		if a, _ := marked(parts, "aptsize"); a != "" {
			if f := strings.Fields(a); len(f) > 0 {
				k, _ := strconv.ParseUint(f[0], 10, 64)
				n = k * 1024
			}
		}
		res.Items = append(res.Items, ServerReclaim{ID: "apt-cache", Size: n, Note: "Downloaded package files that are no longer needed"})
	}
	return res
}

// parseFolders reads `du -xk --max-depth=1`: "size<TAB>path". The folder itself is left out; the biggest come first.
func parseFolders(out, dir string) []ServerFolder {
	folders := []ServerFolder{}
	self := cleanPath(dir)
	for _, ln := range strings.Split(out, "\n") {
		size, p, ok := strings.Cut(ln, "\t")
		if !ok {
			continue
		}
		k, err := strconv.ParseUint(strings.TrimSpace(size), 10, 64)
		if err != nil || cleanPath(p) == self {
			continue
		}
		folders = append(folders, ServerFolder{Path: p, Size: k * 1024})
	}
	sort.SliceStable(folders, func(i, j int) bool { return folders[i].Size > folders[j].Size })
	if len(folders) > maxFolders {
		folders = folders[:maxFolders]
	}
	return folders
}

func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	return p
}

// ---- security ----

// ServerJail is a fail2ban jail.
type ServerJail struct {
	Name        string   `json:"name"`
	Banned      int      `json:"banned"`
	TotalBanned int      `json:"total_banned"`
	IPs         []string `json:"ips"`
}

type serverFail2ban struct {
	Installed bool         `json:"installed"`
	Running   bool         `json:"running"`
	Jails     []ServerJail `json:"jails"`
}

type serverSSHConfig struct {
	Known        bool   `json:"known"`
	Port         int    `json:"port"`
	RootLogin    string `json:"root_login"`
	PasswordAuth string `json:"password_auth"`
}

type serverFailedIP struct {
	IP    string `json:"ip"`
	Count int    `json:"count"`
}

type serverFailedLogins struct {
	Known bool             `json:"known"`
	Total int              `json:"total"`
	Top   []serverFailedIP `json:"top"`
}

// ServerSecurity is the Security section.
type ServerSecurity struct {
	Fail2ban serverFail2ban     `json:"fail2ban"`
	SSH      serverSSHConfig    `json:"ssh"`
	Failed   serverFailedLogins `json:"failed"`
}

const maxFailedIPs = 10

// failedRe finds the address of a failed SSH login; the lines that count are chosen in countsAsFailure.
var failedRe = regexp.MustCompile(`(?:from|rhost=)\s*([0-9A-Fa-f:.]+)`)

// countsAsFailure: one line per attempt. "Failed password for invalid user" repeats the "Invalid user" line of the same
// attempt, and the PAM "authentication failure" line repeats "Failed password", so neither is counted.
func countsAsFailure(ln string) bool {
	switch {
	case strings.Contains(ln, "Failed password for invalid user"):
		return false
	case strings.Contains(ln, "Failed password"), strings.Contains(ln, "Invalid user"):
		return true
	}
	return false
}

// parseSecurity reads the output of securityScript.
func parseSecurity(out string) ServerSecurity {
	parts := splitMarked(out)
	res := ServerSecurity{Fail2ban: serverFail2ban{Jails: []ServerJail{}}, Failed: serverFailedLogins{Top: []serverFailedIP{}}}
	_, res.Fail2ban.Installed = marked(parts, "f2binstalled")
	_, res.Fail2ban.Running = marked(parts, "f2brunning")
	for _, p := range parts {
		if p.name != "jail" || !nameRe.MatchString(p.arg) {
			continue
		}
		j := ServerJail{Name: p.arg, IPs: []string{}}
		for _, ln := range strings.Split(p.body, "\n") {
			_, v, ok := strings.Cut(ln, ":")
			v = strings.TrimSpace(v)
			switch {
			case !ok:
			case strings.Contains(ln, "Currently banned"):
				j.Banned, _ = strconv.Atoi(v)
			case strings.Contains(ln, "Total banned"):
				j.TotalBanned, _ = strconv.Atoi(v)
			case strings.Contains(ln, "Banned IP list"):
				for _, a := range strings.Fields(v) {
					if net.ParseIP(a) != nil {
						j.IPs = append(j.IPs, a)
					}
				}
			}
		}
		res.Fail2ban.Jails = append(res.Fail2ban.Jails, j)
	}
	if sshd, ok := marked(parts, "ssh"); ok {
		for _, ln := range strings.Split(sshd, "\n") {
			f := strings.Fields(ln)
			if len(f) < 2 {
				continue
			}
			switch f[0] {
			case "port":
				if res.SSH.Port == 0 {
					res.SSH.Port, _ = strconv.Atoi(f[1])
				}
			case "permitrootlogin":
				res.SSH.RootLogin = f[1]
			case "passwordauthentication":
				res.SSH.PasswordAuth = f[1]
			}
		}
		res.SSH.Known = res.SSH.Port != 0 || res.SSH.RootLogin != "" || res.SSH.PasswordAuth != ""
	}
	if k, _ := marked(parts, "known"); strings.TrimSpace(k) == "1" {
		res.Failed.Known = true
		auth, _ := marked(parts, "auth")
		count := map[string]int{}
		for _, ln := range strings.Split(auth, "\n") {
			if !countsAsFailure(ln) {
				continue
			}
			m := failedRe.FindStringSubmatch(ln)
			if m == nil {
				continue
			}
			ip := strings.TrimRight(m[1], ":.")
			if net.ParseIP(ip) == nil {
				continue
			}
			count[ip]++
			res.Failed.Total++
		}
		for ip, n := range count {
			res.Failed.Top = append(res.Failed.Top, serverFailedIP{IP: ip, Count: n})
		}
		sort.Slice(res.Failed.Top, func(i, j int) bool {
			if res.Failed.Top[i].Count != res.Failed.Top[j].Count {
				return res.Failed.Top[i].Count > res.Failed.Top[j].Count
			}
			return res.Failed.Top[i].IP < res.Failed.Top[j].IP
		})
		if len(res.Failed.Top) > maxFailedIPs {
			res.Failed.Top = res.Failed.Top[:maxFailedIPs]
		}
	}
	return res
}

// ---- schedule ----

// ServerTimer is a systemd timer.
type ServerTimer struct {
	Name        string `json:"name"` // backup.timer
	Description string `json:"description"`
	Unit        string `json:"unit"` // the unit it starts: backup.service
	Next        string `json:"next"` // as the server prints them, "" when unknown
	Last        string `json:"last"`
	Result      string `json:"result"` // how the last run ended: success, exit-code… ("" when it never ran)
}

// ServerCron is a line of a crontab.
type ServerCron struct {
	Source   string `json:"source"` // crontab (the login's own), root, /etc/crontab or /etc/cron.d/<file>
	User     string `json:"user"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
}

// ServerSchedule is the Schedule section.
type ServerSchedule struct {
	Timers []ServerTimer `json:"timers"`
	Cron   []ServerCron  `json:"cron"`
	Me     string        `json:"me"` // the account of this connection: whose crontab "crontab" is
}

const maxCronLines = 200

func timeProp(v string) string {
	v = strings.TrimSpace(v)
	if v == "n/a" || v == "0" {
		return ""
	}
	return v
}

// parseSchedule reads the output of scheduleScript.
func parseSchedule(out string) ServerSchedule {
	res := ServerSchedule{Timers: []ServerTimer{}, Cron: []ServerCron{}}
	parts := splitMarked(out)
	me, _ := marked(parts, "me")
	res.Me = strings.TrimSpace(me)
	for i, p := range parts {
		switch p.name {
		case "timer":
			t := ServerTimer{Name: p.arg}
			for _, ln := range strings.Split(p.body, "\n") {
				k, v, _ := strings.Cut(ln, "=")
				switch k {
				case "Description":
					t.Description = strings.TrimSpace(v)
				case "Unit":
					t.Unit = strings.TrimSpace(v)
				case "NextElapseUSecRealtime":
					t.Next = timeProp(v)
				case "LastTriggerUSec":
					t.Last = timeProp(v)
				}
			}
			if i+1 < len(parts) && parts[i+1].name == "result" && t.Last != "" {
				if v, ok := strings.CutPrefix(strings.TrimSpace(parts[i+1].body), "Result="); ok {
					t.Result = v
				}
			}
			res.Timers = append(res.Timers, t)
		case "cron":
			withUser := strings.HasPrefix(p.arg, "/etc/")
			user := ""
			if p.arg == "root" {
				user = "root"
			}
			for _, ln := range strings.Split(p.body, "\n") {
				if len(res.Cron) >= maxCronLines {
					break
				}
				if c, ok := parseCronLine(ln, withUser); ok {
					c.Source = p.arg
					if !withUser {
						c.User = user
					}
					res.Cron = append(res.Cron, c)
				}
			}
		}
	}
	return res
}

// parseCronLine reads one crontab line: five time fields or an @word, a user in system files, then the command. Comments,
// blank lines and VAR=value lines are not entries.
func parseCronLine(ln string, withUser bool) (ServerCron, bool) {
	ln = strings.TrimSpace(ln)
	if ln == "" || ln[0] == '#' || !strings.ContainsRune("0123456789*@", rune(ln[0])) {
		return ServerCron{}, false
	}
	f := strings.Fields(ln)
	n := 5
	if ln[0] == '@' {
		n = 1
	}
	skip := n
	if withUser {
		skip++
	}
	if len(f) <= skip {
		return ServerCron{}, false
	}
	c := ServerCron{Schedule: strings.Join(f[:n], " ")}
	if withUser {
		c.User = f[n]
	}
	c.Command = strings.Join(f[skip:], " ")
	return c, true
}

// ---- backup ----

// ServerBackup is the newest file under the backup folder. Now is the clock of the server, so the age does not depend on this computer's.
type ServerBackup struct {
	Found bool   `json:"found"`
	File  string `json:"file"` // relative to the folder
	Size  uint64 `json:"size"`
	MTime int64  `json:"mtime"` // unix seconds
	Now   int64  `json:"now"`
}

// parseBackup reads backupScript: the server clock, then "mtime size name" of the newest file.
func parseBackup(out string) ServerBackup {
	parts := splitMarked(out)
	var res ServerBackup
	now, _ := marked(parts, "now")
	res.Now, _ = strconv.ParseInt(strings.TrimSpace(now), 10, 64)
	newest, _ := marked(parts, "newest")
	f := strings.SplitN(strings.TrimSpace(strings.SplitN(newest, "\n", 2)[0]), " ", 3)
	if len(f) < 3 {
		return res
	}
	t, e1 := strconv.ParseFloat(f[0], 64)
	size, e2 := strconv.ParseUint(f[1], 10, 64)
	if e1 != nil || e2 != nil {
		return res
	}
	res.Found, res.MTime, res.Size, res.File = true, int64(t), size, f[2]
	return res
}

// ---- fleet ----

// ServerFleetHost is one saved host as the fleet overview sees it: a one-off connection that never asks a question.
type ServerFleetHost struct {
	HostID      string         `json:"host_id"`
	Error       string         `json:"error"` // empty when the host answered
	Overview    ServerOverview `json:"overview"`
	FailedUnits int            `json:"failed_units"`
	LatencyMs   int64          `json:"latency_ms"`
}

// parseFleet reads fleetScript: the overview, then the number of failed services.
func parseFleet(out string) (ServerOverview, int) {
	o, _, _ := parseOverview(out)
	n, _ := strconv.Atoi(strings.TrimSpace(section(out, "failed")))
	return o, n
}
