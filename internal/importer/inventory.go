package importer

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// InventoryOptions are what a listing of servers does not say.
type InventoryOptions struct {
	// User is the user name for hosts that do not carry one (AWS and GCP listings do not say which one the image uses).
	User string
	// Address picks which address a cloud instance is reached at: "" or "public" (the public one when it has one, else the
	// private one) or "private".
	Address string
	// Group is the group the hosts go into; empty uses the name of the cloud ("aws", "gcp"). An Ansible inventory brings its own groups.
	Group string
	// IncludeStopped also imports instances that are not running.
	IncludeStopped bool
}

// Inventory formats ParseInventory recognises.
const (
	FormatAWS     = "AWS EC2 (aws ec2 describe-instances)"
	FormatGCP     = "Google Cloud (gcloud compute instances list)"
	FormatAnsible = "Ansible inventory"
	FormatCSV     = "CSV"
)

const maxInventoryHosts = 5000

// ParseInventory reads a listing of servers that a command produced (or a spreadsheet exported) and returns the hosts it describes,
// in the form the ssh config import uses, with the name of the format it found. It never contacts a cloud: the listing is
// made by you with your own tools and credentials.
func ParseInventory(data []byte, o InventoryOptions) (*Parsed, string, error) {
	data = bytes.TrimPrefix(bytes.TrimSpace(data), []byte("\xef\xbb\xbf"))
	if len(data) == 0 {
		return nil, "", errors.New("the file is empty")
	}
	var p *Parsed
	var format string
	var err error
	switch {
	case data[0] == '{' || data[0] == '[':
		p, format, err = parseJSONInventory(data, o)
	case looksLikeCSV(data):
		p, format, err = parseCSVInventory(data)
	default:
		p, format, err = parseINIInventory(data)
	}
	if err != nil {
		return nil, "", err
	}
	checkEntries(p)
	if len(p.Entries) == 0 {
		return nil, format, fmt.Errorf("no host was found in this %s listing", format)
	}
	if len(p.Entries) > maxInventoryHosts {
		return nil, format, fmt.Errorf("the listing has %d hosts: import at most %d at a time", len(p.Entries), maxInventoryHosts)
	}
	return p, format, nil
}

var (
	validAddr = regexp.MustCompile(`^[A-Za-z0-9_.:%\-\[\]]+$`)
	validUser = regexp.MustCompile(`^[A-Za-z0-9_.@+\-]*$`)
)

// checkEntries drops what could not be saved as a usable host (a listing is somebody else's text): an address or a user name
// with a space or a shell character in it, a port out of range. Each is reported, and the rest of the listing goes on.
func checkEntries(p *Parsed) {
	kept := p.Entries[:0]
	bad := 0
	for _, e := range p.Entries {
		e.Alias = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, e.Alias)
		if len(e.Alias) > 100 {
			e.Alias = e.Alias[:100]
		}
		switch {
		case e.Alias == "", len(e.HostName) > 253, strings.HasPrefix(e.HostName, "-"), !validAddr.MatchString(e.HostName),
			len(e.User) > 64, strings.HasPrefix(e.User, "-"), !validUser.MatchString(e.User), e.Port < 0 || e.Port > 65535:
			bad++
			continue
		}
		kept = append(kept, e)
	}
	p.Entries = kept
	note(p, bad, "host has an address or a user name that cannot be used and was skipped", "hosts have an address or a user name that cannot be used and were skipped")
}

func parseJSONInventory(data []byte, o InventoryOptions) (*Parsed, string, error) {
	var any interface{}
	if err := json.Unmarshal(data, &any); err != nil {
		return nil, "", fmt.Errorf("this is not valid JSON: %v", err)
	}
	switch v := any.(type) {
	case map[string]interface{}:
		if _, ok := v["Reservations"]; ok {
			return awsInstances(reservations(v), o), FormatAWS, nil
		}
		if _, ok := v["_meta"]; ok || hasHostsGroup(v) {
			p, err := ansibleJSON(v, o)
			return p, FormatAnsible, err
		}
	case []interface{}:
		if len(v) > 0 {
			if m, ok := v[0].(map[string]interface{}); ok {
				if _, ok := m["InstanceId"]; ok {
					return awsInstances(asMaps(v), o), FormatAWS, nil
				}
				if _, ok := m["networkInterfaces"]; ok {
					return gcpInstances(asMaps(v), o), FormatGCP, nil
				}
				if _, ok := m["Instances"]; ok { // --query 'Reservations[]'
					return awsInstances(reservations(map[string]interface{}{"Reservations": v}), o), FormatAWS, nil
				}
			}
		}
	}
	return nil, "", errors.New("this JSON is not a listing Vutty knows: use the output of aws ec2 describe-instances, gcloud compute instances list --format=json or ansible-inventory --list")
}

func asMaps(a []interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	for _, x := range a {
		if m, ok := x.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func str(m map[string]interface{}, k string) string {
	if s, ok := m[k].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// ---- AWS ----

func reservations(v map[string]interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	rs, _ := v["Reservations"].([]interface{})
	for _, r := range rs {
		if rm, ok := r.(map[string]interface{}); ok {
			is, _ := rm["Instances"].([]interface{})
			out = append(out, asMaps(is)...)
		}
	}
	return out
}

func awsTags(inst map[string]interface{}) map[string]string {
	tags := map[string]string{}
	ts, _ := inst["Tags"].([]interface{})
	for _, t := range ts {
		if m, ok := t.(map[string]interface{}); ok {
			if k := str(m, "Key"); k != "" {
				tags[k] = str(m, "Value")
			}
		}
	}
	return tags
}

func awsInstances(insts []map[string]interface{}, o InventoryOptions) *Parsed {
	p := &Parsed{}
	group := firstNonEmpty(o.Group, "aws")
	user := firstNonEmpty(o.User, "ec2-user")
	used := map[string]int{}
	stopped, noAddr := 0, 0
	keys := map[string]bool{}
	for _, in := range insts {
		state := ""
		if st, ok := in["State"].(map[string]interface{}); ok {
			state = str(st, "Name")
		}
		if state != "" && state != "running" && !o.IncludeStopped {
			stopped++
			continue
		}
		addr := pickAddress(o.Address, str(in, "PublicIpAddress"), str(in, "PublicDnsName"), str(in, "PrivateIpAddress"))
		if addr == "" {
			noAddr++
			continue
		}
		tags := awsTags(in)
		id := str(in, "InstanceId")
		e := Entry{
			Alias: uniqueLabel(firstNonEmpty(tags["Name"], id), id, used), HostName: addr, User: user, Source: group,
			Env: envFromTags(tags), Tags: tagList(tags, "Name", "Environment", "environment", "Env", "env", "Stage", "stage"),
		}
		if k := str(in, "KeyName"); k != "" {
			keys[k] = true
		}
		p.Entries = append(p.Entries, e)
	}
	note(p, stopped, "instance that is not running was skipped (tick Include stopped instances to import it too)", "instances that are not running were skipped (tick Include stopped instances to import them too)")
	note(p, noAddr, "instance has no address of the kind chosen and was skipped", "instances have no address of the kind chosen and were skipped")
	if len(keys) > 0 {
		var names []string
		for k := range keys {
			names = append(names, k)
		}
		sort.Strings(names)
		p.Warnings = append(p.Warnings, "key pairs used: "+strings.Join(names, ", ")+". Add the key in Keychain and pick it on the hosts; the listing does not carry it.")
	}
	return p
}

// ---- Google Cloud ----

func gcpInstances(insts []map[string]interface{}, o InventoryOptions) *Parsed {
	p := &Parsed{}
	group := firstNonEmpty(o.Group, "gcp")
	used := map[string]int{}
	stopped, noAddr := 0, 0
	for _, in := range insts {
		if st := str(in, "status"); st != "" && st != "RUNNING" && !o.IncludeStopped {
			stopped++
			continue
		}
		var priv, pub string
		if nics, ok := in["networkInterfaces"].([]interface{}); ok && len(nics) > 0 {
			if nic, ok := nics[0].(map[string]interface{}); ok {
				priv = str(nic, "networkIP")
				if acs, ok := nic["accessConfigs"].([]interface{}); ok && len(acs) > 0 {
					if ac, ok := acs[0].(map[string]interface{}); ok {
						pub = str(ac, "natIP")
					}
				}
			}
		}
		addr := pickAddress(o.Address, pub, "", priv)
		if addr == "" {
			noAddr++
			continue
		}
		labels := map[string]string{}
		if lm, ok := in["labels"].(map[string]interface{}); ok {
			for k, v := range lm {
				if s, ok := v.(string); ok {
					labels[k] = s
				}
			}
		}
		name := str(in, "name")
		p.Entries = append(p.Entries, Entry{
			Alias: uniqueLabel(name, str(in, "id"), used), HostName: addr, User: o.User, Source: group,
			Env: envFromTags(labels), Tags: tagList(labels, "env", "environment", "stage"),
		})
	}
	note(p, stopped, "instance that is not running was skipped (tick Include stopped instances to import it too)", "instances that are not running were skipped (tick Include stopped instances to import them too)")
	note(p, noAddr, "instance has no address of the kind chosen and was skipped", "instances have no address of the kind chosen and were skipped")
	return p
}

// ---- shared helpers ----

func pickAddress(mode, publicIP, publicDNS, private string) string {
	if mode == "private" {
		return private
	}
	return firstNonEmpty(publicIP, firstNonEmpty(publicDNS, private))
}

func note(p *Parsed, n int, one, many string) {
	switch {
	case n == 1:
		p.Warnings = append(p.Warnings, "1 "+one)
	case n > 1:
		p.Warnings = append(p.Warnings, fmt.Sprintf("%d %s", n, many))
	}
}

// uniqueLabel keeps two instances with the same name apart inside one import.
func uniqueLabel(name, id string, used map[string]int) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = id
	}
	used[name]++
	if used[name] == 1 {
		return name
	}
	if id != "" {
		return name + " (" + id + ")"
	}
	return name + " (" + strconv.Itoa(used[name]) + ")"
}

func envFromTags(tags map[string]string) string {
	for _, k := range []string{"Environment", "environment", "Env", "env", "Stage", "stage"} {
		switch strings.ToLower(strings.TrimSpace(tags[k])) {
		case "prod", "production", "prd":
			return "prod"
		case "staging", "stage", "stg", "preprod", "uat":
			return "staging"
		case "dev", "development", "test", "testing":
			return "dev"
		}
	}
	return ""
}

// tagList turns the other tags into key=value tags (short ones only: a tag is a label, not a record).
func tagList(tags map[string]string, skip ...string) []string {
	skipSet := map[string]bool{}
	for _, k := range skip {
		skipSet[k] = true
	}
	var keys []string
	for k := range tags {
		if !skipSet[k] && !strings.HasPrefix(k, "aws:") && !strings.HasPrefix(k, "goog-") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		t := k
		if v := strings.TrimSpace(tags[k]); v != "" {
			t = k + "=" + v
		}
		if len(t) <= 40 && !strings.ContainsAny(t, ", \t\r\n") {
			out = append(out, t)
		}
		if len(out) == 8 {
			break
		}
	}
	return out
}

// ---- Ansible ----

func hasHostsGroup(v map[string]interface{}) bool {
	for k, g := range v {
		if k == "_meta" {
			continue
		}
		if gm, ok := g.(map[string]interface{}); ok {
			if _, ok := gm["hosts"]; ok {
				return true
			}
		}
	}
	return false
}

func intOf(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	}
	return 0
}

// ansibleJSON reads the output of ansible-inventory --list (which also covers what a dynamic cloud plugin produces).
func ansibleJSON(v map[string]interface{}, o InventoryOptions) (*Parsed, error) {
	p := &Parsed{}
	hostvars := map[string]map[string]interface{}{}
	if meta, ok := v["_meta"].(map[string]interface{}); ok {
		if hv, ok := meta["hostvars"].(map[string]interface{}); ok {
			for h, vars := range hv {
				if m, ok := vars.(map[string]interface{}); ok {
					hostvars[h] = m
				}
			}
		}
	}
	// a host belongs to the first real group it is in (all and ungrouped are not groups)
	groupOf := map[string]string{}
	var groups []string
	for g := range v {
		if g != "_meta" && g != "all" && g != "ungrouped" {
			groups = append(groups, g)
		}
	}
	sort.Strings(groups)
	var order []string
	seen := map[string]bool{}
	add := func(h string) {
		if !seen[h] {
			seen[h] = true
			order = append(order, h)
		}
	}
	for _, g := range groups {
		gm, _ := v[g].(map[string]interface{})
		hs, _ := gm["hosts"].([]interface{})
		for _, h := range hs {
			if name, ok := h.(string); ok {
				if _, taken := groupOf[name]; !taken {
					groupOf[name] = g
				}
				add(name)
			}
		}
	}
	for _, g := range []string{"all", "ungrouped"} {
		if gm, ok := v[g].(map[string]interface{}); ok {
			if hs, ok := gm["hosts"].([]interface{}); ok {
				for _, h := range hs {
					if name, ok := h.(string); ok {
						add(name)
					}
				}
			}
		}
	}
	for h := range hostvars {
		add(h)
	}
	skipped := 0
	used := map[string]int{}
	for _, name := range order {
		vars := hostvars[name]
		if c := str(vars, "ansible_connection"); c != "" && c != "ssh" && c != "smart" && c != "paramiko" {
			skipped++
			continue
		}
		addr := firstNonEmpty(str(vars, "ansible_host"), str(vars, "ansible_ssh_host"))
		if addr == "" { // what a dynamic cloud inventory carries
			addr = pickAddress(o.Address, str(vars, "public_ip_address"), str(vars, "public_dns_name"), str(vars, "private_ip_address"))
		}
		if addr == "" {
			addr = name
		}
		tags := map[string]string{}
		if tm, ok := vars["tags"].(map[string]interface{}); ok {
			for k, x := range tm {
				if s, ok := x.(string); ok {
					tags[k] = s
				}
			}
		}
		port := intOf(vars["ansible_port"])
		if port == 0 {
			port = intOf(vars["ansible_ssh_port"])
		}
		p.Entries = append(p.Entries, Entry{
			Alias: uniqueLabel(firstNonEmpty(tags["Name"], name), "", used), HostName: addr, Port: port,
			User:   firstNonEmpty(str(vars, "ansible_user"), firstNonEmpty(str(vars, "ansible_ssh_user"), o.User)),
			Source: firstNonEmpty(o.Group, groupOf[name]), Env: envFromTags(tags),
		})
	}
	note(p, skipped, "host does not use SSH (ansible_connection) and was skipped", "hosts do not use SSH (ansible_connection) and were skipped")
	return p, nil
}

// iniHost is what the first word of a host line may look like: a name or address, with a range such as [01:03] allowed.
var iniHost = regexp.MustCompile(`^[A-Za-z0-9_.:\-\[\]]+$`)

var rangeRE = regexp.MustCompile(`\[(\d+):(\d+)\]`)

// expandRange turns web[01:03].example.com into three names (numbers only, at most 256).
func expandRange(name string) []string {
	m := rangeRE.FindStringSubmatchIndex(name)
	if m == nil {
		return []string{name}
	}
	lo, hi := name[m[2]:m[3]], name[m[4]:m[5]]
	a, _ := strconv.Atoi(lo)
	b, _ := strconv.Atoi(hi)
	if b < a || b-a >= 256 {
		return []string{name}
	}
	var out []string
	for i := a; i <= b; i++ {
		n := strconv.Itoa(i)
		for len(n) < len(lo) {
			n = "0" + n
		}
		out = append(out, expandRange(name[:m[0]]+n+name[m[1]:])...)
	}
	return out
}

// kvFields splits host variables (key=value, a value may be quoted).
func kvFields(s string) []string {
	var out []string
	var cur strings.Builder
	quote := rune(0)
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ' ' || r == '\t':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func parseINIInventory(data []byte) (*Parsed, string, error) {
	p := &Parsed{}
	group := ""
	skipSection := false
	used := map[string]int{}
	notHosts := 0
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-1])
			skipSection = strings.Contains(name, ":") // [group:vars] and [group:children] carry no hosts
			group = name
			if name == "all" || name == "ungrouped" {
				group = ""
			}
			continue
		}
		if skipSection {
			continue
		}
		fields := kvFields(line)
		if len(fields) == 0 {
			continue
		}
		if !iniHost.MatchString(fields[0]) { // a line that is not a host (a CSV, a config file…)
			notHosts++
			continue
		}
		vars := map[string]string{}
		for _, f := range fields[1:] {
			if k, v, ok := strings.Cut(f, "="); ok {
				vars[k] = v
			}
		}
		if c := vars["ansible_connection"]; c != "" && c != "ssh" && c != "smart" && c != "paramiko" {
			continue
		}
		for _, name := range expandRange(fields[0]) {
			port, _ := strconv.Atoi(firstNonEmpty(vars["ansible_port"], vars["ansible_ssh_port"]))
			p.Entries = append(p.Entries, Entry{
				Alias: uniqueLabel(name, "", used), HostName: firstNonEmpty(vars["ansible_host"], firstNonEmpty(vars["ansible_ssh_host"], name)), Port: port,
				User: firstNonEmpty(vars["ansible_user"], vars["ansible_ssh_user"]), Source: group,
			})
		}
		if len(p.Entries) > maxInventoryHosts {
			break
		}
	}
	if len(p.Entries) == 0 {
		return nil, FormatAnsible, errors.New("no host was found: this is not an Ansible inventory, a JSON listing or a CSV with a hostname column")
	}
	note(p, notHosts, "line is not a host and was skipped", "lines are not hosts and were skipped")
	return p, FormatAnsible, nil
}

// ---- CSV ----

// looksLikeCSV: the first line is a header that names an address column (alone, or with other columns).
func looksLikeCSV(data []byte) bool {
	first := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	for _, f := range strings.Split(strings.ToLower(first), ",") {
		switch strings.TrimSpace(f) {
		case "hostname", "address", "host":
			return true
		}
	}
	return false
}

func parseCSVInventory(data []byte) (*Parsed, string, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	head, err := r.Read()
	if err != nil {
		return nil, "", fmt.Errorf("the CSV has no header line: %v", err)
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	pick := func(rec []string, names ...string) string {
		for _, n := range names {
			if i, ok := col[n]; ok && i < len(rec) {
				if v := strings.TrimSpace(rec[i]); v != "" {
					return v
				}
			}
		}
		return ""
	}
	if _, ok := col["hostname"]; !ok {
		if _, ok := col["address"]; !ok {
			if _, ok := col["host"]; !ok {
				return nil, "", errors.New("the CSV needs a hostname column (hostname, address or host); the others are optional: label, user, port, group, env, tags")
			}
		}
	}
	p := &Parsed{}
	used := map[string]int{}
	for line := 2; ; line++ {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			p.Warnings = append(p.Warnings, fmt.Sprintf("line %d: %v", line, err))
			continue
		}
		host := pick(rec, "hostname", "address", "host")
		if host == "" {
			continue
		}
		port, _ := strconv.Atoi(pick(rec, "port"))
		var tags []string
		for _, t := range strings.FieldsFunc(pick(rec, "tags"), func(r rune) bool { return r == ';' || r == '|' || r == ' ' }) {
			tags = append(tags, t)
		}
		p.Entries = append(p.Entries, Entry{
			Alias: uniqueLabel(firstNonEmpty(pick(rec, "label", "name"), host), "", used), HostName: host, Port: port,
			User: pick(rec, "user", "username"), Source: pick(rec, "group"), Env: envFromTags(map[string]string{"env": pick(rec, "env", "environment")}), Tags: tags,
		})
		if len(p.Entries) > maxInventoryHosts {
			break
		}
	}
	return p, FormatCSV, nil
}
