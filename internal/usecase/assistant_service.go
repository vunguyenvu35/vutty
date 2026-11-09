package usecase

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"vutty/internal/domain/assistant"
	"vutty/internal/domain/secretpolicy"
	"vutty/internal/fsutil"
)

// ErrConfirmShare is returned by Ask when the host's policy is "ask" and the user has not yet confirmed that
// the screen may be shared in this session. The UI shows the preview and calls ConfirmShare.
var ErrConfirmShare = errors.New("confirm_share")

const (
	maxQuestionRunes  = 4000
	maxSelectionRunes = 20000
	maxHistoryTurns   = 8
	modelsTTL         = 24 * time.Hour
	maxResultBytes    = 32 << 10
)

var modelID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@\[\]-]{0,99}$`)

// AssistantService is the AI terminal assistant. It never lets raw terminal text reach an agent: everything
// goes through secretpolicy.Guard first, and everything an agent says goes through it again before it is
// shown, stored or run. See docs/design/ai-assistant.md.
type AssistantService struct {
	c    *Core
	sess *SessionService

	mu        sync.Mutex
	cfg       assistant.Config
	loaded    bool
	detected  bool
	detecting bool
	status    map[string]assistant.Status
	models    map[string]modelCache
	convs     map[string]*conversation
	confirmed map[string]bool
	runs      map[string]*assistRun
	mcpState  map[string]string
	flights   map[string]*sync.Mutex // one model load per provider at a time
	seq       int
}

type modelCache struct {
	list []assistant.Model
	ok   bool // the list was loaded: a failure is not kept for the whole TTL, it is tried again
	err  string
	at   time.Time
}

// modelsRetryAfter is how long a failed load is remembered before the next call tries again.
const modelsRetryAfter = 3 * time.Second

type conversation struct {
	provider string
	turns    []assistant.Turn
}

func NewAssistantService(c *Core, s *SessionService) *AssistantService {
	return &AssistantService{c: c, sess: s, status: map[string]assistant.Status{}, models: map[string]modelCache{},
		convs: map[string]*conversation{}, confirmed: map[string]bool{}, runs: map[string]*assistRun{}, mcpState: map[string]string{}, flights: map[string]*sync.Mutex{}}
}

// ---- configuration ----

func (a *AssistantService) file() string { return filepath.Join(a.c.DataDir, "assistant.json") }

func (a *AssistantService) cfgLocked() assistant.Config {
	if !a.loaded {
		b, _ := os.ReadFile(a.file())
		cfg, err := assistant.ParseConfig(b)
		if err != nil {
			cfg = assistant.DefaultConfig() // an unreadable file never blocks the app: everything stays off
		}
		a.cfg, a.loaded = cfg, true
	}
	return a.cfg
}

// config returns a snapshot. It is replaced, never edited in place, so callers may read it freely.
func (a *AssistantService) config() assistant.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfgLocked()
}

// update edits a copy of the configuration and stores it.
func (a *AssistantService) update(fn func(c *assistant.Config) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, _ := json.Marshal(a.cfgLocked())
	var cp assistant.Config
	if err := json.Unmarshal(b, &cp); err != nil {
		return err
	}
	cp.Normalize()
	if err := fn(&cp); err != nil {
		return err
	}
	cp.Normalize()
	out, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(a.file(), out, 0o600); err != nil {
		return err
	}
	a.cfg = cp
	return nil
}

// AssistantSettings are the options of the Settings page that are not about one provider.
type AssistantSettings struct {
	Lines           int      `json:"lines"`
	Patterns        []string `json:"patterns"`
	AutoRun         bool     `json:"auto_run"`
	DefaultProvider string   `json:"default_provider"`
}

func (a *AssistantService) Settings() AssistantSettings {
	c := a.config()
	return AssistantSettings{Lines: c.Lines, Patterns: append([]string{}, c.Patterns...), AutoRun: c.AutoRun, DefaultProvider: c.DefaultProvider}
}

// SaveSettings validates and stores the options. Patterns can only add detectors.
func (a *AssistantService) SaveSettings(in AssistantSettings) (AssistantSettings, error) {
	var pats []string
	for _, p := range in.Patterns {
		if strings.TrimSpace(p) != "" {
			pats = append(pats, p)
		}
	}
	if _, err := (secretpolicy.Guard{}).WithPatterns(pats); err != nil {
		return AssistantSettings{}, err
	}
	err := a.update(func(c *assistant.Config) error {
		c.Lines, c.Patterns, c.AutoRun = in.Lines, pats, in.AutoRun
		if in.DefaultProvider != "" {
			if pc := c.Providers[in.DefaultProvider]; !pc.Enabled {
				return errors.New("the default provider must be enabled")
			}
			c.DefaultProvider = in.DefaultProvider
		}
		return nil
	})
	return a.Settings(), err
}
