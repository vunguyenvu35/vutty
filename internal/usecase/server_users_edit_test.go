package usecase

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testKey(t *testing.T, comment string) (line, fp string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))) + " " + comment, ssh.FingerprintSHA256(sp)
}

func TestParseAuthorizedKeyTellsAPrivateKeyAndGarbageApart(t *testing.T) {
	line, fp := testKey(t, "me@laptop")
	k, err := parseAuthorizedKey(line)
	if err != nil || k.Type != "ssh-ed25519" || k.Comment != "me@laptop" || k.Fingerprint != fp {
		t.Fatalf("got %+v, %v", k, err)
	}
	if k, err := parseAuthorizedKey(`from="10.0.0.1" ` + line); err != nil || !k.Options {
		t.Fatalf("options not seen: %+v, %v", k, err)
	}
	for in, want := range map[string]string{
		"":                                    "paste a public key",
		"-----BEGIN OPENSSH PRIVATE KEY-----": "private key",
		"hello world":                         "not a public key",
		line + "\n" + line:                    "one line",
	} {
		if _, err := parseAuthorizedKey(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", in, err, want)
		}
	}
}

func TestAddAndRemoveKeepOtherLinesAndRefuseADuplicate(t *testing.T) {
	a, fa := testKey(t, "a")
	b, fb := testKey(t, "b")
	file := "# my keys\n" + a + "\nnot a key at all\n"
	next, err := addKeyTo(file, b)
	if err != nil || len(parseUserKeys(next)) != 2 {
		t.Fatalf("add: %q, %v", next, err)
	}
	if _, err := addKeyTo(next, strings.Fields(b)[0]+" "+strings.Fields(b)[1]+" other-comment"); err == nil {
		t.Fatal("the same key under another comment was added twice")
	}
	left, found := removeKeyFrom(next, fa)
	if !found || strings.Contains(left, strings.Fields(a)[1]) || !strings.Contains(left, "# my keys") || !strings.Contains(left, "not a key at all") || !strings.Contains(left, fb[:0]+strings.Fields(b)[1]) {
		t.Fatalf("remove: found=%v %q", found, left)
	}
	if _, found := removeKeyFrom(left, fa); found {
		t.Fatal("removed a key twice")
	}
	if s, err := addKeyTo("", a); err != nil || s != a+"\n" {
		t.Fatalf("add to an empty file: %q %v", s, err)
	}
	if s, _ := addKeyTo("x", a); !strings.HasPrefix(s, "x\n") {
		t.Fatalf("a last line without a newline: %q", s)
	}
}

func TestUserNamesAndShellsThatReachAScript(t *testing.T) {
	for _, ok := range []string{"deploy", "_svc", "a-b_c1"} {
		if !userNameRe.MatchString(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "Root", "1abc", "a b", "a;rm", "a$(id)", "a\nb", strings.Repeat("a", 33), "-x"} {
		if userNameRe.MatchString(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, bad := range []string{"bash", "/bin/bash; id", "/bin/ba sh", "/"} {
		if userShellRe.MatchString(bad) {
			t.Errorf("shell %q accepted", bad)
		}
	}
	s := &ServerService{}
	if err := s.AddUser("x", "bad name", "/bin/bash", false, ""); err == nil || !strings.Contains(err.Error(), "user name") {
		t.Errorf("AddUser: %v", err)
	}
	if err := s.AddUser("x", "ok", "/bin/bash", false, "-----BEGIN OPENSSH PRIVATE KEY-----"); err == nil || !strings.Contains(err.Error(), "private key") {
		t.Errorf("AddUser with a private key: %v", err)
	}
	if err := s.DeleteUser("x", "a;b", false); err == nil {
		t.Error("DeleteUser accepted a bad name")
	}
	if err := s.RemoveUserKey("x", "deploy", "not-a-fingerprint"); err == nil {
		t.Error("RemoveUserKey accepted a bad fingerprint")
	}
}

func TestParseUsersReadsTheAccountOfThisConnection(t *testing.T) {
	if got := parseUsers("##passwd\n##me\ndeploy\n").Me; got != "deploy" {
		t.Fatalf("Me = %q", got)
	}
}
