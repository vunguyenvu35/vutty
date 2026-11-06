package usecase

import "testing"

func TestParseSystem(t *testing.T) {
	r := parseSystem("##host\nweb-1\n##tz\nAsia/Ho_Chi_Minh\n##systemd\nyes\n##swap\n/swapfile file 2147483648 1024 -2\n/dev/sda2 partition 1073741824 0 -3\n##ram\nMemTotal:        2000000 kB\n##disk\n/dev/vda1 41152736 10000000 20480000 33% /\n")
	if r.Hostname != "web-1" || r.Timezone != "Asia/Ho_Chi_Minh" || !r.Systemd || r.SwapFile != 2147483648 || r.SwapTotal != 3221225472 || !r.SwapOther || r.RAM != 2000000*1024 || r.DiskFree != 20480000*1024 {
		t.Fatalf("%+v", r)
	}
	if q := parseSystem("##host\nx\n##swap\n"); q.SwapTotal != 0 || q.Systemd {
		t.Fatalf("%+v", q)
	}
}

func TestSystemEditsRefuseBeforeTouchingTheServer(t *testing.T) {
	s := &ServerService{sessions: map[string]*serverSession{}}
	for _, h := range []string{"", "Web", "a b", "-a", "a_b", "x;reboot"} {
		if err := s.SetHostname("x", h); err == nil || err.Error() == "the connection to the server is closed: reconnect" {
			t.Fatalf("hostname %q: %v", h, err)
		}
	}
	for _, z := range []string{"", "a b", "../etc", "UTC;ls"} {
		if err := s.SetTimezone("x", z); err == nil || err.Error() == "the connection to the server is closed: reconnect" {
			t.Fatalf("zone %q: %v", z, err)
		}
	}
	for _, mb := range []int{0, 100, 99999} {
		if err := s.SwapCreate("x", mb); err == nil || err.Error() == "the connection to the server is closed: reconnect" {
			t.Fatalf("swap %d: %v", mb, err)
		}
	}
}
