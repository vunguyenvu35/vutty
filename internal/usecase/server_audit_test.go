package usecase

import "testing"

func TestParseAudit(t *testing.T) {
	a := parseAudit("fw=ufw\nauto=no\nuid0=toor \nemptypw=guest bob \nnopasswd=2\nreboot=yes\n")
	if a.Firewall != "ufw" || a.AutoUpd != "no" || len(a.UID0) != 1 || a.UID0[0] != "toor" || !a.PWKnown || len(a.EmptyPW) != 2 || a.NoPasswd != 2 || !a.Reboot {
		t.Fatalf("%+v", a)
	}
	b := parseAudit("fw=none\nauto=none\nuid0=\nemptypw=?\nnopasswd=?\nreboot=no\n")
	if b.PWKnown || b.NoPasswd != -1 || len(b.UID0) != 0 || b.Reboot {
		t.Fatalf("%+v", b)
	}
}

func TestParseLogins(t *testing.T) {
	out := `root     pts/0        203.0.113.7      Fri Nov  6 09:00:12 2026   still logged in
ubuntu   pts/1        198.51.100.4     Thu Nov  5 22:10:01 2026 - Thu Nov  5 22:40:00 2026  (00:29)
reboot   system boot  5.15.0           Thu Nov  5 20:00:00 2026   still running

wtmp begins Mon Oct  1 00:00:00 2026
`
	l := parseLogins(out)
	if len(l) != 2 || l[0].User != "root" || l[0].From != "203.0.113.7" || l[0].When != "Fri Nov 6 09:00:12 2026" || l[0].Status != "still logged in" || l[1].User != "ubuntu" {
		t.Fatalf("%+v", l)
	}
}
