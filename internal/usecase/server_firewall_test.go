package usecase

import (
	"reflect"
	"testing"
)

func TestParseFirewallRules(t *testing.T) {
	ufw := `Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 22/tcp                     ALLOW IN    Anywhere
[ 2] 80,443/tcp                 ALLOW IN    10.0.0.0/8
[ 3] 5432                       DENY IN     Anywhere
[ 4] 22/tcp (v6)                ALLOW IN    Anywhere (v6)
`
	want := []FirewallRule{{1, "22/tcp", "ALLOW", "Anywhere"}, {2, "80,443/tcp", "ALLOW", "10.0.0.0/8"}, {3, "5432", "DENY", "Anywhere"}}
	if got := parseFirewallRules(ufw); !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	p := parsePorts("##ss\nLISTEN 0 1 0.0.0.0:80 0.0.0.0:*\nLISTEN 0 1 0.0.0.0:5432 0.0.0.0:*\n##ufw\n" + ufw)
	if len(p.Rules) != 3 || !p.Ports[0].Open || p.Ports[1].Open {
		t.Fatalf("%+v", p)
	}
}

func TestUfwRuleArgs(t *testing.T) {
	ok := []struct {
		a    string
		port int
		pr   string
		from string
		want string
	}{
		{"allow", 8080, "tcp", "", "allow 8080/tcp"},
		{"deny", 53, "", "", "deny 53"},
		{"allow", 22, "tcp", "10.0.0.0/8", "allow from 10.0.0.0/8 to any port 22 proto tcp"},
		{"allow", 22, "", "1.2.3.4", "allow from 1.2.3.4 to any port 22"},
	}
	for _, c := range ok {
		if got, err := ufwRuleArgs(c.a, c.port, c.pr, c.from); err != nil || got != c.want {
			t.Fatalf("%+v: %q %v", c, got, err)
		}
	}
	for _, c := range [][4]any{{"allow", 0, "", ""}, {"allow", 70000, "", ""}, {"open", 80, "", ""}, {"allow", 80, "icmp", ""}, {"allow", 80, "", "1.2.3.4; reboot"}} {
		if _, err := ufwRuleArgs(c[0].(string), c[1].(int), c[2].(string), c[3].(string)); err == nil {
			t.Fatalf("%v should be refused", c)
		}
	}
}

func TestRulePorts(t *testing.T) {
	if got := rulePorts("80,443/tcp"); !reflect.DeepEqual(got, []int{80, 443}) {
		t.Fatal(got)
	}
	if got := rulePorts("8000:8002/tcp"); !reflect.DeepEqual(got, []int{8000, 8001, 8002}) {
		t.Fatal(got)
	}
	if got := rulePorts("22/tcp (v6)"); !reflect.DeepEqual(got, []int{22}) {
		t.Fatal(got)
	}
}
