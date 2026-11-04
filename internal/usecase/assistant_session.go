package usecase

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
	"vutty/internal/domain"
	"vutty/internal/domain/assistant"
	"vutty/internal/domain/secretpolicy"
)

// ---- hosts and sessions ----

func (a *AssistantService) lookupHost(hostID string) (domain.Host, bool) {
	id := strings.TrimPrefix(hostID, telnetPrefix)
	if hostID == LocalHostID || strings.HasPrefix(hostID, serialPrefix) {
		return domain.Host{}, false
	}
	h, err := a.c.Store.GetHost(id)
	return h, err == nil
}

// SessionState tells the panel what it may do in one terminal.
type SessionState struct {
	HostID     string `json:"host_id"`
	Label      string `json:"label"`
	Policy     string `json:"policy"` // effective: off | ask | on
	Stored     string `json:"stored"` // what is saved on the host ("" = default)
	Production bool   `json:"production"`
	Confirmed  bool   `json:"confirmed"`
	Connected  bool   `json:"connected"`
}

func (a *AssistantService) SessionInfo(sid string) (SessionState, error) {
	st, err := a.sess.screen(sid)
	if err != nil {
		return SessionState{}, err
	}
	h, isHost := a.lookupHost(st.hostID)
	prod := isHost && h.Env == domain.EnvProd
	stored := ""
	if isHost {
		stored = h.AIPolicy
	}
	a.mu.Lock()
	conf := a.confirmed[sid]
	a.mu.Unlock()
	return SessionState{HostID: st.hostID, Label: a.c.hostLabel(st.hostID), Policy: string(assistant.EffectivePolicy(stored, prod)),
		Stored: stored, Production: prod, Confirmed: conf, Connected: st.state == "ready"}, nil
}

// SetHostPolicy stores the assistant switch of a host: "" (default), off, ask or on.
func (a *AssistantService) SetHostPolicy(hostID, policy string) error {
	if !assistant.ValidPolicy(policy) {
		return errors.New("policy must be off, ask or on")
	}
	h, err := a.c.Store.GetHost(hostID)
	if err != nil {
		return fmt.Errorf("host: %w", err)
	}
	h.AIPolicy = policy
	_, err = a.c.Store.SaveHost(h)
	return err
}

// ConfirmShare records that the user saw the preview and agreed to share this terminal in this session.
func (a *AssistantService) ConfirmShare(sid string) error {
	if _, err := a.sess.screen(sid); err != nil {
		return err
	}
	a.mu.Lock()
	a.confirmed[sid] = true
	a.mu.Unlock()
	return nil
}

// ForgetSession drops the conversation of a closed terminal.
func (a *AssistantService) ForgetSession(sid string) {
	a.mu.Lock()
	delete(a.convs, sid)
	delete(a.confirmed, sid)
	a.mu.Unlock()
}

// ResetConversation starts a new chat in a terminal.
func (a *AssistantService) ResetConversation(sid string) {
	a.mu.Lock()
	delete(a.convs, sid)
	a.mu.Unlock()
}

// ---- guard and context ----

func (a *AssistantService) guard() (secretpolicy.Guard, bool, error) {
	g := secretpolicy.Guard{}
	locked := true
	if a.c.Vault.Status().Unlocked {
		if secrets, err := a.c.Vault.DumpSecrets(); err == nil {
			vals := make([][]byte, 0, len(secrets))
			for _, s := range secrets {
				vals = append(vals, s.Value)
			}
			g = g.WithSecrets(vals)
			locked = false
		}
	}
	g, err := g.WithPatterns(a.config().Patterns)
	return g, locked, err
}

func (a *AssistantService) facts(hostID string) string {
	var b strings.Builder
	if h, ok := a.lookupHost(hostID); ok {
		fmt.Fprintf(&b, "Host: %s (%s)\n", h.Label, h.Hostname)
		if h.Username != "" {
			fmt.Fprintf(&b, "User: %s\n", h.Username)
		}
		if h.OS != "" {
			fmt.Fprintf(&b, "Operating system: %s\n", h.OS)
		}
		if h.Env != "" {
			fmt.Fprintf(&b, "Environment: %s\n", h.Env)
		}
	} else {
		fmt.Fprintf(&b, "Host: %s\n", a.c.hostLabel(hostID))
	}
	return b.String()
}

// PreviewView is exactly what would be sent as the terminal context, already redacted.
type PreviewView struct {
	Text        string                 `json:"text"`
	Findings    []secretpolicy.Finding `json:"findings"`
	Redacted    int                    `json:"redacted"`
	Lines       int                    `json:"lines"`
	AltScreen   bool                   `json:"alt_screen"`
	VaultLocked bool                   `json:"vault_locked"`
	Empty       bool                   `json:"empty"`
	// Images is how many pictures would be sent as they are (they are not part of Text: nothing can be hidden in them).
	Images int `json:"images"`
}

func (a *AssistantService) screenText(sid string, lines int, selection string) (text string, alt bool, hostID string, err error) {
	st, err := a.sess.screen(sid)
	if err != nil {
		return "", false, "", err
	}
	if selection != "" {
		if utf8.RuneCountInString(selection) > maxSelectionRunes {
			return "", false, "", errors.New("the selection is too long")
		}
		return selection, false, st.hostID, nil
	}
	if lines <= 0 {
		lines = a.config().Lines
	}
	if lines > 1000 {
		lines = 1000
	}
	text, alt = assistant.ScreenText(st.raw, lines)
	return text, alt, st.hostID, nil
}

// Preview shows what the assistant would receive. Nothing is sent.
func (a *AssistantService) Preview(sid string, lines int, selection string, attachments []assistant.Attachment) (PreviewView, error) {
	raw, alt, hostID, err := a.screenText(sid, lines, selection)
	if err != nil {
		return PreviewView{}, err
	}
	fileText, images, err := attached(attachments)
	if err != nil {
		return PreviewView{}, err
	}
	raw += fileText
	g, locked, err := a.guard()
	if err != nil {
		return PreviewView{}, err
	}
	san, fs, err := g.Redact(a.facts(hostID) + "\n" + raw)
	if err != nil {
		return PreviewView{}, errors.New("Not sent: redaction failed")
	}
	if fs == nil {
		fs = []secretpolicy.Finding{}
	}
	return PreviewView{Text: san.String(), Findings: fs, Redacted: secretpolicy.Total(fs), Lines: strings.Count(raw, "\n") + 1,
		AltScreen: alt, VaultLocked: locked, Empty: strings.TrimSpace(raw) == "", Images: len(images)}, nil
}
