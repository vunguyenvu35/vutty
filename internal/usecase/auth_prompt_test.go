package usecase

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"vutty/internal/testssh"
)

// a bastion that wants the password and then a verification code
func codeServer(t *testing.T) *testssh.Server {
	t.Helper()
	srv, err := testssh.Start(testssh.Config{User: "root", Password: "pw", Code: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv
}

// the questions of the first term:auth event for key
func (h *harness) authQuestions(t *testing.T, key string) (host string, prompts []string) {
	t.Helper()
	e := h.rec.wait(t, "term:auth:"+key)
	b, _ := json.Marshal(e.data) // what the screen receives
	var m struct {
		Host    string
		Prompts []struct {
			Text string
			Echo bool
		}
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, p := range m.Prompts {
		prompts = append(prompts, p.Text)
	}
	return m.Host, prompts
}

func fmtAny(v any) string { return fmt.Sprintf("%v", v) }

func TestTerminalAsksForTheVerificationCodeAndSignsInWithIt(t *testing.T) {
	h := newHarness(t).unlocked(t)
	host := h.sshHost(t, "bastion", codeServer(t), "")
	sid, err := h.open(host.ID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	h.trust(t, sid)
	who, qs := h.authQuestions(t, sid)
	if who != "bastion" || len(qs) != 1 || !strings.Contains(qs[0], "Verification code") {
		t.Fatalf("asked %q %v: the saved password should have answered the password already", who, qs)
	}
	if err := h.app.Session.AnswerAuth(sid, []string{"123456"}); err != nil {
		t.Fatal(err)
	}
	h.rec.waitOutput(t, sid, "READY")
}

func TestAWrongVerificationCodeEndsTheConnectionWithAReason(t *testing.T) {
	h := newHarness(t).unlocked(t)
	host := h.sshHost(t, "bastion", codeServer(t), "")
	sid, _ := h.open(host.ID, 80, 24)
	h.trust(t, sid)
	h.authQuestions(t, sid)
	if err := h.app.Session.AnswerAuth(sid, []string{"000000"}); err != nil {
		t.Fatal(err)
	}
	e := h.rec.wait(t, "term:closed:"+sid)
	if r := fmtAny(e.data); !strings.Contains(strings.ToLower(r), "authenticate") {
		t.Fatalf("closed with %q", r)
	}
}

func TestCancellingTheQuestionClosesTheConnection(t *testing.T) {
	h := newHarness(t).unlocked(t)
	host := h.sshHost(t, "bastion", codeServer(t), "")
	sid, _ := h.open(host.ID, 80, 24)
	h.trust(t, sid)
	h.authQuestions(t, sid)
	if err := h.app.Session.AnswerAuth(sid, nil); err != nil {
		t.Fatal(err)
	}
	e := h.rec.wait(t, "term:closed:"+sid)
	if r := fmtAny(e.data); !strings.Contains(r, "cancelled") {
		t.Fatalf("closed with %q", r)
	}
}

func TestAnAnswerWithNoQuestionWaitingIsRefused(t *testing.T) {
	h := newHarness(t).unlocked(t)
	if err := h.app.Session.AnswerAuth("nothing", []string{"x"}); err == nil {
		t.Fatal("an answer with no question was accepted")
	}
}

func TestAQuestionAskedBeforeTheScreenListenedIsAskedAgainOnAttach(t *testing.T) {
	h := newHarness(t).unlocked(t)
	host := h.sshHost(t, "bastion", codeServer(t), "")
	sid, err := h.app.Session.Open(host.ID, 80, 24) // not attached yet: nobody hears the first event
	if err != nil {
		t.Fatal(err)
	}
	h.trust(t, sid)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := h.core.authPrompts.payload(sid); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.rec.mu.Lock()
	h.rec.events = nil // the screen was not there to hear it
	h.rec.mu.Unlock()
	if err := h.app.Session.Attach(sid); err != nil {
		t.Fatal(err)
	}
	if _, qs := h.authQuestions(t, sid); len(qs) != 1 {
		t.Fatalf("the question was not replayed: %v", qs)
	}
	_ = h.app.Session.AnswerAuth(sid, []string{"123456"})
	h.rec.waitOutput(t, sid, "READY")
}
