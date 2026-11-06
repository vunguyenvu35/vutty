package usecase

import (
	"reflect"
	"testing"
)

func TestParseSiteFiles(t *testing.T) {
	got := parseSiteFiles("on example.com\noff default\nbogus x\non ba;d\n")
	want := []WebSiteFile{{"example.com", true}, {"default", false}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
}

func TestCheckCertRequest(t *testing.T) {
	if err := checkCertRequest([]string{"example.com", "www.example.com"}, "me@example.com"); err != nil {
		t.Fatal(err)
	}
	bad := [][]string{{}, {"*.example.com"}, {"example"}, {"a.com; reboot"}, {"-a.com"}, {"a b.com"}}
	for _, d := range bad {
		if checkCertRequest(d, "me@example.com") == nil {
			t.Fatalf("%v should be refused", d)
		}
	}
	for _, e := range []string{"", "me", "me@x", "a b@x.com", "$(id)@x.com", "me@x.com;ls"} {
		if checkCertRequest([]string{"a.com"}, e) == nil {
			t.Fatalf("%q should be refused", e)
		}
	}
}

func TestWebEditRefuseBadNamesBeforeTouchingTheServer(t *testing.T) {
	s := &ServerService{sessions: map[string]*serverSession{}}
	if err := s.WebSiteSet("x", "a b", true); err == nil || err.Error() != "that is not a site name" {
		t.Fatal(err)
	}
	if _, err := s.CertRenew("x", "../../etc", false); err == nil || err.Error() != "that is not a certificate name" {
		t.Fatal(err)
	}
	if _, err := s.CertRequest("x", []string{"nope"}, "a@b.co"); err == nil {
		t.Fatal("refused")
	}
}
