package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"

	"vutty/internal/testssh"
)

// asked records the rounds a test Asker was given and answers each question with the next of its answers.
type asked struct {
	mu      sync.Mutex
	rounds  []Challenge
	answers []string
	err     error
}

func (a *asked) ask(c Challenge) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rounds = append(a.rounds, c)
	if a.err != nil {
		return nil, a.err
	}
	out := make([]string, len(c.Prompts))
	for i := range out {
		if len(a.answers) > 0 {
			out[i], a.answers = a.answers[0], a.answers[1:]
		}
	}
	return out, nil
}

func startCode(t *testing.T, cfg testssh.Config) *testssh.Server {
	t.Helper()
	cfg.User = "root"
	s, err := testssh.Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func dialHop(t *testing.T, h Hop) error {
	t.Helper()
	d, _ := newDialer(t, true)
	c, err := d.Dial([]Hop{h})
	if err == nil {
		c.Close()
	}
	return err
}

func TestPasswordThenVerificationCodeAsksOnlyForTheCode(t *testing.T) {
	s := startCode(t, testssh.Config{Password: "pw", Code: "123456"})
	a := &asked{answers: []string{"123456"}}
	h := hopFor("bastion", s, SavedPassword("pw")...)
	h.Password, h.Ask = "pw", a.ask
	if err := dialHop(t, h); err != nil {
		t.Fatal(err)
	}
	if len(a.rounds) != 1 || len(a.rounds[0].Prompts) != 1 {
		t.Fatalf("the user was asked %+v, want one question", a.rounds)
	}
	if p := a.rounds[0].Prompts[0]; strings.TrimSpace(p.Text) != "Verification code:" || p.Echo {
		t.Fatalf("%+v", p)
	}
	if a.rounds[0].Host != "bastion" {
		t.Fatalf("the question does not say which machine asks: %q", a.rounds[0].Host)
	}
}

func TestAWrongCodeIsRefusedAndTheUserIsNotAskedForeverForThePassword(t *testing.T) {
	s := startCode(t, testssh.Config{Password: "pw", Code: "123456"})
	a := &asked{answers: []string{"000000"}}
	h := hopFor("t", s, SavedPassword("pw")...)
	h.Password, h.Ask = "pw", a.ask
	if err := dialHop(t, h); err == nil {
		t.Fatal("a wrong code signed in")
	}
	if len(a.rounds) == 0 || len(a.rounds) > 3 {
		t.Fatalf("asked %d times", len(a.rounds))
	}
}

func TestAKeyThenVerificationCodeAsksForTheCode(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	sp, _ := ssh.NewPublicKey(pub)
	s := startCode(t, testssh.Config{AuthKeys: []ssh.PublicKey{sp}, Code: "424242"})
	a := &asked{answers: []string{"424242"}}
	h := hopFor("t", s, KeyAuth(signer))
	h.Ask = a.ask
	if err := dialHop(t, h); err != nil {
		t.Fatal(err)
	}
	if len(a.rounds) != 1 {
		t.Fatalf("asked %d times", len(a.rounds))
	}
}

func TestAKeyAloneNeverAsksAnything(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	sp, _ := ssh.NewPublicKey(pub)
	s := startCode(t, testssh.Config{AuthKeys: []ssh.PublicKey{sp}})
	a := &asked{}
	h := hopFor("t", s, KeyAuth(signer))
	h.Ask = a.ask
	if err := dialHop(t, h); err != nil {
		t.Fatal(err)
	}
	if len(a.rounds) != 0 {
		t.Fatalf("a key that is enough was followed by a question: %+v", a.rounds)
	}
}

func TestPasswordAndCodeAskedInOneRoundLikePAM(t *testing.T) {
	s := startCode(t, testssh.Config{Password: "pw", Code: "777777", PAM: true})
	a := &asked{answers: []string{"777777"}}
	h := hopFor("t", s)
	h.Password, h.Ask = "pw", a.ask
	if err := dialHop(t, h); err != nil {
		t.Fatal(err)
	}
	if len(a.rounds) != 1 || len(a.rounds[0].Prompts) != 1 || !strings.Contains(a.rounds[0].Prompts[0].Text, "Verification") {
		t.Fatalf("the saved password should have answered the first question: %+v", a.rounds)
	}
}

func TestWithoutAPasswordSavedEveryQuestionGoesToTheUser(t *testing.T) {
	s := startCode(t, testssh.Config{Password: "pw", Code: "777777", PAM: true})
	a := &asked{answers: []string{"pw", "777777"}}
	h := hopFor("t", s)
	h.Ask = a.ask
	if err := dialHop(t, h); err != nil {
		t.Fatal(err)
	}
	if len(a.rounds) != 1 || len(a.rounds[0].Prompts) != 2 {
		t.Fatalf("%+v", a.rounds)
	}
}

func TestACancelledQuestionStopsTheLogin(t *testing.T) {
	s := startCode(t, testssh.Config{Password: "pw", Code: "123456"})
	a := &asked{err: errors.New("sign-in cancelled")}
	h := hopFor("t", s, SavedPassword("pw")...)
	h.Password, h.Ask = "pw", a.ask
	if err := dialHop(t, h); err == nil {
		t.Fatal("signed in without answering")
	}
}

func TestAServerThatNeedsACodeSaysSoWhenThereIsNobodyToAsk(t *testing.T) {
	s := startCode(t, testssh.Config{Password: "pw", Code: "123456"})
	h := hopFor("t", s, SavedPassword("pw")...)
	h.Password = "pw" // no Ask: a background connection
	err := dialHop(t, h)
	if err == nil || !strings.Contains(err.Error(), "cannot ask you") {
		t.Fatalf("want the reason, got %v", err)
	}
}

func TestLooksLikeCode(t *testing.T) {
	for q, want := range map[string]bool{
		"Password: ": false, "Password for root@host: ": false, "root@host's password: ": false,
		"Verification code: ": true, "Enter PASSCODE: ": true, "Duo passcode or option (1-3): ": true, "One-time password (OATH) for root: ": true,
		"Enter your PIN: ": true, "Authenticator code": true, "Token: ": true, "Response: ": true, "Spinning wheel: ": false,
	} {
		if got := looksLikeCode(q); got != want {
			t.Errorf("%q: got %v, want %v", q, got, want)
		}
	}
}
