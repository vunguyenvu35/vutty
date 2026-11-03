package importer

import (
	"fmt"
	"strings"
	"testing"

	"vutty/internal/domain"
)

const awsSample = `{"Reservations":[
 {"Instances":[
  {"InstanceId":"i-0a1","State":{"Name":"running"},"PublicIpAddress":"54.1.2.3","PrivateIpAddress":"10.0.0.11","KeyName":"prod-key",
   "Tags":[{"Key":"Name","Value":"web-1"},{"Key":"Environment","Value":"production"},{"Key":"Team","Value":"infra"},{"Key":"aws:cloudformation:stack-name","Value":"x"}]},
  {"InstanceId":"i-0a2","State":{"Name":"running"},"PrivateIpAddress":"10.0.0.12","KeyName":"prod-key","Tags":[{"Key":"Name","Value":"db-1"},{"Key":"env","Value":"stg"}]}
 ]},
 {"Instances":[
  {"InstanceId":"i-0a3","State":{"Name":"stopped"},"PrivateIpAddress":"10.0.0.13","Tags":[{"Key":"Name","Value":"old"}]},
  {"InstanceId":"i-0a4","State":{"Name":"running"},"PublicIpAddress":"54.1.2.9","Tags":[{"Key":"Name","Value":"web-1"}]},
  {"InstanceId":"i-0a5","State":{"Name":"running"}}
 ]}
]}`

func byLabel(p *Parsed) map[string]Entry {
	m := map[string]Entry{}
	for _, e := range p.Entries {
		m[e.Alias] = e
	}
	return m
}

func TestAWSDescribeInstances(t *testing.T) {
	p, format, err := ParseInventory([]byte(awsSample), InventoryOptions{})
	if err != nil || format != FormatAWS {
		t.Fatalf("%v %q", err, format)
	}
	m := byLabel(p)
	if len(p.Entries) != 3 { // old is stopped, i-0a5 has no address
		t.Fatalf("%d entries: %+v", len(p.Entries), p.Entries)
	}
	w := m["web-1"]
	if w.HostName != "54.1.2.3" || w.User != "ec2-user" || w.Source != "aws" || w.Env != "prod" {
		t.Fatalf("%+v", w)
	}
	if len(w.Tags) != 1 || w.Tags[0] != "Team=infra" { // Name and Environment are used, aws: tags are left out
		t.Fatalf("tags %v", w.Tags)
	}
	if d := m["db-1"]; d.HostName != "10.0.0.12" || d.Env != "staging" { // no public address: the private one
		t.Fatalf("%+v", d)
	}
	if dup, ok := m["web-1 (i-0a4)"]; !ok || dup.HostName != "54.1.2.9" { // the same name twice
		t.Fatalf("labels %v", labelsOf(p))
	}
	joined := strings.Join(p.Warnings, "\n")
	for _, want := range []string{"1 instance that is not running was skipped", "1 instance has no address", "key pairs used: prod-key"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no warning with %q in:\n%s", want, joined)
		}
	}
}

func labelsOf(p *Parsed) []string {
	var out []string
	for _, e := range p.Entries {
		out = append(out, e.Alias)
	}
	return out
}

func TestAWSOptions(t *testing.T) {
	p, _, _ := ParseInventory([]byte(awsSample), InventoryOptions{Address: "private", User: "ubuntu", Group: "prod-fleet", IncludeStopped: true})
	m := byLabel(p)
	if m["web-1"].HostName != "10.0.0.11" || m["web-1"].User != "ubuntu" || m["web-1"].Source != "prod-fleet" {
		t.Fatalf("%+v", m["web-1"])
	}
	if _, ok := m["old"]; !ok {
		t.Fatalf("a stopped instance was left out although it was asked for: %v", labelsOf(p))
	}
	if _, ok := m["web-1 (i-0a4)"]; ok { // no private address: skipped
		t.Fatalf("an instance without a private address was imported: %v", labelsOf(p))
	}
}

func TestAWSQueryShapesAreRecognisedToo(t *testing.T) {
	flat := `[{"InstanceId":"i-1","State":{"Name":"running"},"PublicIpAddress":"1.1.1.1"}]` // --query 'Reservations[].Instances[]'
	p, format, err := ParseInventory([]byte(flat), InventoryOptions{})
	if err != nil || format != FormatAWS || len(p.Entries) != 1 || p.Entries[0].Alias != "i-1" { // no Name tag: the id names it
		t.Fatalf("%v %q %+v", err, format, p)
	}
	rs := `[{"Instances":[{"InstanceId":"i-2","State":{"Name":"running"},"PrivateIpAddress":"10.1.1.1"}]}]` // --query 'Reservations[]'
	if p, _, err := ParseInventory([]byte(rs), InventoryOptions{}); err != nil || len(p.Entries) != 1 {
		t.Fatalf("%v %+v", err, p)
	}
}

func TestGoogleCloudInstancesList(t *testing.T) {
	gcp := `[{"id":"123","name":"api-1","status":"RUNNING","labels":{"env":"prod","team":"core"},
	  "networkInterfaces":[{"networkIP":"10.128.0.5","accessConfigs":[{"natIP":"34.1.2.3"}]}]},
	 {"id":"124","name":"api-2","status":"TERMINATED","networkInterfaces":[{"networkIP":"10.128.0.6"}]},
	 {"id":"125","name":"batch","status":"RUNNING","networkInterfaces":[{"networkIP":"10.128.0.7"}]}]`
	p, format, err := ParseInventory([]byte(gcp), InventoryOptions{User: "me"})
	if err != nil || format != FormatGCP || len(p.Entries) != 2 {
		t.Fatalf("%v %q %+v", err, format, p)
	}
	m := byLabel(p)
	if a := m["api-1"]; a.HostName != "34.1.2.3" || a.Env != "prod" || a.User != "me" || a.Source != "gcp" || len(a.Tags) != 1 || a.Tags[0] != "team=core" {
		t.Fatalf("%+v", a)
	}
	if m["batch"].HostName != "10.128.0.7" {
		t.Fatalf("%+v", m["batch"])
	}
	if !strings.Contains(strings.Join(p.Warnings, ""), "1 instance that is not running was skipped") {
		t.Fatalf("%v", p.Warnings)
	}
}

const ansibleListJSON = `{
 "_meta":{"hostvars":{
   "web1":{"ansible_host":"10.0.1.1","ansible_user":"deploy","ansible_port":2222},
   "web2":{"ansible_host":"10.0.1.2"},
   "db1":{"ansible_host":"10.0.2.1","ansible_user":"dba"},
   "tool":{"ansible_connection":"local"},
   "ec2-9":{"public_ip_address":"3.3.3.3","private_ip_address":"10.9.9.9","tags":{"Name":"cache-1","Environment":"dev"}}
 }},
 "all":{"children":["ungrouped","web","db"]},
 "ungrouped":{"hosts":["tool","ec2-9"]},
 "web":{"hosts":["web1","web2"]},
 "db":{"hosts":["db1","web2"]}
}`

func TestAnsibleInventoryJSON(t *testing.T) {
	p, format, err := ParseInventory([]byte(ansibleListJSON), InventoryOptions{})
	if err != nil || format != FormatAnsible {
		t.Fatalf("%v %q", err, format)
	}
	m := byLabel(p)
	if len(m) != 5-1 { // the local connection is skipped
		t.Fatalf("%v", labelsOf(p))
	}
	if w := m["web1"]; w.HostName != "10.0.1.1" || w.User != "deploy" || w.Port != 2222 || w.Source != "web" {
		t.Fatalf("%+v", w)
	}
	if w := m["web2"]; w.Source != "db" { // in two groups: the first by name
		t.Fatalf("%+v", w)
	}
	if c := m["cache-1"]; c.HostName != "3.3.3.3" || c.Env != "dev" || c.Source != "" { // a dynamic cloud inventory
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(strings.Join(p.Warnings, ""), "1 host does not use SSH") {
		t.Fatalf("%v", p.Warnings)
	}
	over, _, _ := ParseInventory([]byte(ansibleListJSON), InventoryOptions{Group: "fleet", User: "ops", Address: "private"})
	if o := byLabel(over); o["web1"].Source != "fleet" || o["web2"].User != "ops" || o["cache-1"].HostName != "10.9.9.9" {
		t.Fatalf("%+v", o)
	}
}

const ansibleINI = `
# production fleet
standalone.example.com
bastion ansible_host=203.0.113.9 ansible_user=ops

[web]
web[01:03].example.com ansible_user="deploy" ansible_port=2200
; a comment

[db]
db1 ansible_host=10.0.2.1 ansible_connection=local
db2 ansible_host=10.0.2.2

[web:vars]
ansible_user=ignored

[prod:children]
web
db
`

func TestAnsibleInventoryINI(t *testing.T) {
	p, format, err := ParseInventory([]byte(ansibleINI), InventoryOptions{})
	if err != nil || format != FormatAnsible {
		t.Fatalf("%v %q", err, format)
	}
	m := byLabel(p)
	want := []string{"standalone.example.com", "bastion", "web01.example.com", "web02.example.com", "web03.example.com", "db2"}
	if len(p.Entries) != len(want) {
		t.Fatalf("%v", labelsOf(p))
	}
	for _, w := range want {
		if _, ok := m[w]; !ok {
			t.Errorf("%s is missing: %v", w, labelsOf(p))
		}
	}
	if b := m["bastion"]; b.HostName != "203.0.113.9" || b.User != "ops" || b.Source != "" {
		t.Fatalf("%+v", b)
	}
	if w := m["web02.example.com"]; w.User != "deploy" || w.Port != 2200 || w.Source != "web" || w.HostName != "web02.example.com" {
		t.Fatalf("%+v", w)
	}
	if d := m["db2"]; d.HostName != "10.0.2.2" || d.Source != "db" {
		t.Fatalf("%+v", d)
	}
}

func TestAnEntryThatCannotBeUsedIsSkippedWithAWarningAndTheRestGoesOn(t *testing.T) {
	csvText := "hostname,user,port,label\n" +
		"good.example.com,deploy,22,good\n" +
		"bad host.example.com,deploy,22,space-in-address\n" +
		"-oProxyCommand=evil,deploy,22,dash-address\n" +
		"x.example.com,de ploy,22,space-in-user\n" +
		"y.example.com,-root,22,dash-user\n" +
		"z.example.com;rm,deploy,22,shell-address\n" +
		"w.example.com,deploy,99999,big-port\n" +
		"ok6.example.com,,22,\"line\x01break\"\n"
	p, _, err := ParseInventory([]byte(csvText), InventoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := labelsOf(p)
	if len(got) != 2 || got[0] != "good" || got[1] != "linebreak" { // a control character is taken out of a label
		t.Fatalf("%v", got)
	}
	if !strings.Contains(strings.Join(p.Warnings, ""), "6 hosts have an address or a user name that cannot be used") {
		t.Fatalf("%v", p.Warnings)
	}
	if _, _, err := ParseInventory([]byte("hostname\nbad host\n"), InventoryOptions{}); err == nil {
		t.Fatal("a listing with nothing usable was accepted")
	}
}

func TestExpandRange(t *testing.T) {
	for in, want := range map[string]string{
		"web[1:3]":      "web1 web2 web3",
		"n[08:10].x":    "n08.x n09.x n10.x",
		"a[1:2]-b[1:2]": "a1-b1 a1-b2 a2-b1 a2-b2",
		"plain":         "plain",
		"r[5:1]":        "r[5:1]", // backwards: left as it is
		"big[1:1000]":   "big[1:1000]",
		"letters[a:c]":  "letters[a:c]",
	} {
		if got := strings.Join(expandRange(in), " "); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestCSVListing(t *testing.T) {
	csvText := "Label,Hostname,User,Port,Group,Env,Tags\n" +
		"web-1,10.0.0.1,deploy,2222,web,PROD,a;b\n" +
		",10.0.0.2,,,,staging,\n" +
		"\"db, main\",db.example.com,dba,,db,development,\"x y\"\n" +
		",,,,,,\n"
	p, format, err := ParseInventory([]byte(csvText), InventoryOptions{})
	if err != nil || format != FormatCSV || len(p.Entries) != 3 {
		t.Fatalf("%v %q %+v", err, format, p)
	}
	m := byLabel(p)
	if w := m["web-1"]; w.HostName != "10.0.0.1" || w.User != "deploy" || w.Port != 2222 || w.Source != "web" || w.Env != "prod" || len(w.Tags) != 2 {
		t.Fatalf("%+v", w)
	}
	if h := m["10.0.0.2"]; h.Env != "staging" { // no label: the address names it
		t.Fatalf("%+v", h)
	}
	if d := m["db, main"]; d.Env != "dev" || len(d.Tags) != 2 {
		t.Fatalf("%+v", d)
	}
	if _, _, err := ParseInventory([]byte("name,owner\nweb,me\n"), InventoryOptions{}); err == nil {
		t.Fatal("a CSV with no address column was accepted")
	}
}

func TestInventoryRefusesWhatIsNotAListing(t *testing.T) {
	big := "{\"Reservations\":[{\"Instances\":["
	for i := 0; i <= maxInventoryHosts; i++ {
		if i > 0 {
			big += ","
		}
		big += fmt.Sprintf(`{"InstanceId":"i-%d","State":{"Name":"running"},"PrivateIpAddress":"10.0.%d.%d"}`, i, i/250, i%250)
	}
	big += "]}]}"
	for name, in := range map[string]string{
		"empty": "", "spaces": "  \n ", "bad json": "{not json", "unknown json": `{"hello":"world"}`, "unknown array": `[1,2,3]`,
		"no hosts": `{"Reservations":[]}`, "only vars": "[all:vars]\nansible_user=x\n", "too many": big,
	} {
		if p, _, err := ParseInventory([]byte(in), InventoryOptions{}); err == nil {
			t.Errorf("%s was accepted: %+v", name, p)
		}
	}
}

func TestInventoryBecomesGroupsAndHostsThroughBuild(t *testing.T) {
	p, _, _ := ParseInventory([]byte(awsSample), InventoryOptions{})
	plan := Build(p, domain.Data{}, PolicySkip)
	if len(plan.Hosts) != 3 || len(plan.Groups) != 1 || plan.Groups[0].Name != "aws" {
		t.Fatalf("%d hosts %+v", len(plan.Hosts), plan.Groups)
	}
	for _, h := range plan.Hosts {
		if h.GroupID != plan.Groups[0].ID || h.Port != 22 || h.Hostname == "" {
			t.Fatalf("%+v", h)
		}
	}
	// a second import: the same labels are skipped, or kept as copies
	existing := domain.Data{Hosts: plan.Hosts, Groups: plan.Groups}
	if again := Build(p, existing, PolicySkip); len(again.Hosts) != 0 || len(again.Skipped) != 3 {
		t.Fatalf("%d hosts %v", len(again.Hosts), again.Skipped)
	}
	if again := Build(p, existing, PolicyDuplicate); len(again.Hosts) != 3 || len(again.Groups) != 0 {
		t.Fatalf("%d hosts %d groups", len(again.Hosts), len(again.Groups))
	}
}
