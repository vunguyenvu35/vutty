package usecase

import "testing"

func TestMergeSSHDropIn(t *testing.T) {
	got := mergeSSHDropIn("", "PermitRootLogin", "no")
	if got != "PermitRootLogin no\n" {
		t.Fatalf("%q", got)
	}
	got = mergeSSHDropIn("permitrootlogin yes\nPasswordAuthentication no\n", "PermitRootLogin", "no")
	if got != "PasswordAuthentication no\nPermitRootLogin no\n" {
		t.Fatalf("%q", got)
	}
}

func TestSecurityEditsRefuseBeforeTouchingTheServer(t *testing.T) {
	s := &ServerService{sessions: map[string]*serverSession{}}
	for _, c := range [][2]string{{"port", "22"}, {"root_login", "maybe"}, {"password_auth", "no; reboot"}} {
		if err := s.SSHSet("x", c[0], c[1]); err == nil || err.Error() != "that sshd setting cannot be changed here" {
			t.Fatalf("%v: %v", c, err)
		}
	}
	if err := s.Ban("x", "sshd", "not-an-ip"); err == nil || err.Error() != "that is not an IP address" {
		t.Fatal(err)
	}
	if err := s.Ban("x", "sshd", "127.0.0.1"); err == nil {
		t.Fatal("loopback")
	}
	if err := s.Ban("x", "a b", "1.2.3.4"); err == nil {
		t.Fatal("jail")
	}
}
