package usecase

import (
	"errors"
	"sort"
	"strings"
	"vutty/internal/domain"
	"vutty/internal/domain/assistant"
)

// ---- managing MCP servers (Settings) ----

type McpServerView struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	URL       string   `json:"url"`
	Env       []string `json:"env"`     // names only: values are write-only
	Headers   []string `json:"headers"` // names only
	Enabled   bool     `json:"enabled"`
	Trusted   bool     `json:"trusted"`
	HostIDs   []string `json:"host_ids"`
	Providers []string `json:"providers"`
	Status    string   `json:"status"`
	Reviewed  int      `json:"reviewed"`
}

type McpServerInput struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	URL       string            `json:"url"`
	Env       map[string]string `json:"env"`     // a non-empty value is stored in the vault
	Headers   map[string]string `json:"headers"` // likewise
	RemoveEnv []string          `json:"remove_env"`
	RemoveHdr []string          `json:"remove_headers"`
	Enabled   bool              `json:"enabled"`
	Trusted   bool              `json:"trusted"`
	HostIDs   []string          `json:"host_ids"`
	Providers []string          `json:"providers"`
}

type McpToolView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReadOnly    bool   `json:"read_only"`
	Mode        string `json:"mode"` // the stored mode, or the suggested one for a new tool
	New         bool   `json:"new"`  // not reviewed yet: stays off until a mode is chosen
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (a *AssistantService) mcpView(s assistant.MCPServer) McpServerView {
	a.mu.Lock()
	st := a.mcpState[s.ID]
	a.mu.Unlock()
	return McpServerView{ID: s.ID, Name: s.Name, Type: s.Type, Command: s.Command, Args: orEmpty(s.Args), URL: s.URL,
		Env: sortedKeys(s.EnvRefs), Headers: sortedKeys(s.HeaderRefs), Enabled: s.Enabled, Trusted: s.Trusted,
		HostIDs: orEmpty(s.HostIDs), Providers: orEmpty(s.Providers), Status: st, Reviewed: len(s.Tools)}
}

func (a *AssistantService) McpServers() []McpServerView {
	out := []McpServerView{}
	for _, s := range a.config().MCP {
		out = append(out, a.mcpView(s))
	}
	return out
}

func (a *AssistantService) findMCP(id string) (assistant.MCPServer, bool) {
	for _, s := range a.config().MCP {
		if s.ID == id {
			return s, true
		}
	}
	return assistant.MCPServer{}, false
}

// storeSecrets writes the given values into the vault and returns the updated name → secret id map.
func (a *AssistantService) storeSecrets(refs map[string]string, values map[string]string, remove []string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range refs {
		out[k] = v
	}
	for _, name := range remove {
		if id, ok := out[name]; ok {
			_ = a.c.Vault.DeleteSecret(id)
			delete(out, name)
		}
	}
	for name, val := range values {
		if val == "" {
			continue
		}
		if id, ok := out[name]; ok {
			if err := a.c.Vault.PutSecret(id, domain.SecretPassword, []byte(val)); err != nil {
				return nil, err
			}
			continue
		}
		id, err := a.c.Vault.AddSecret(domain.SecretPassword, []byte(val))
		if err != nil {
			return nil, err
		}
		out[name] = id
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// SaveMcpServer adds or changes a server. Environment and header values go to the vault and are never read
// back; an empty value keeps the stored one. A new server is off and untrusted until the user says otherwise.
func (a *AssistantService) SaveMcpServer(in McpServerInput) (McpServerView, error) {
	needsVault := len(in.RemoveEnv)+len(in.RemoveHdr) > 0
	for _, v := range in.Env {
		needsVault = needsVault || v != ""
	}
	for _, v := range in.Headers {
		needsVault = needsVault || v != ""
	}
	if needsVault && !a.c.Vault.Status().Unlocked {
		return McpServerView{}, errNotUnlocked
	}
	var saved assistant.MCPServer
	err := a.update(func(c *assistant.Config) error {
		s := assistant.MCPServer{ID: in.ID}
		idx := -1
		for i, x := range c.MCP {
			if x.ID == in.ID && in.ID != "" {
				s, idx = x, i
			}
		}
		if in.ID != "" && idx < 0 {
			return errors.New("unknown MCP server")
		}
		if idx < 0 {
			s.ID = domain.NewID()
		}
		s.Name, s.Type, s.Command, s.Args, s.URL = strings.TrimSpace(in.Name), in.Type, strings.TrimSpace(in.Command), in.Args, strings.TrimSpace(in.URL)
		s.Enabled, s.Trusted, s.HostIDs, s.Providers = in.Enabled, in.Trusted, in.HostIDs, in.Providers
		for _, x := range c.MCP {
			if x.ID != s.ID && x.Slug() == s.Slug() {
				return errors.New("another server already has this name")
			}
		}
		var err error
		if s.EnvRefs, err = a.storeSecrets(s.EnvRefs, in.Env, in.RemoveEnv); err != nil {
			return err
		}
		if s.HeaderRefs, err = a.storeSecrets(s.HeaderRefs, in.Headers, in.RemoveHdr); err != nil {
			return err
		}
		if err := s.Validate(); err != nil {
			return err
		}
		if idx < 0 {
			c.MCP = append(c.MCP, s)
		} else {
			c.MCP[idx] = s
		}
		saved = s
		return nil
	})
	if err != nil {
		return McpServerView{}, err
	}
	return a.mcpView(saved), nil
}

// DeleteMcpServer removes a server and its secrets.
func (a *AssistantService) DeleteMcpServer(id string) error {
	s, ok := a.findMCP(id)
	if !ok {
		return errors.New("unknown MCP server")
	}
	err := a.update(func(c *assistant.Config) error {
		var keep []assistant.MCPServer
		for _, x := range c.MCP {
			if x.ID != id {
				keep = append(keep, x)
			}
		}
		c.MCP = keep
		return nil
	})
	if err == nil && a.c.Vault.Status().Unlocked {
		for _, sid := range s.EnvRefs {
			_ = a.c.Vault.DeleteSecret(sid)
		}
		for _, sid := range s.HeaderRefs {
			_ = a.c.Vault.DeleteSecret(sid)
		}
	}
	return err
}

func suggestedMode(t assistant.Tool) assistant.ToolMode {
	if t.ReadOnly {
		return assistant.ToolAllow
	}
	return assistant.ToolAsk
}

// TestMcpServer starts the server, lists its tools and stops it. Nothing is sent to an AI provider.
func (a *AssistantService) TestMcpServer(id string) ([]McpToolView, error) {
	s, ok := a.findMCP(id)
	if !ok {
		return nil, errors.New("unknown MCP server")
	}
	srv, err := a.connectMCP(s)
	if err == nil {
		defer srv.Close()
	}
	var tools []assistant.Tool
	if err == nil {
		tools, err = srv.Tools()
	}
	a.mu.Lock()
	if err != nil {
		a.mcpState[id] = "Unavailable: " + err.Error()
	} else {
		a.mcpState[id] = "Connected"
	}
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	out := []McpToolView{}
	for _, t := range tools {
		v := McpToolView{Name: t.Name, Description: t.Description, ReadOnly: t.ReadOnly}
		if m, ok := s.Tools[t.Name]; ok {
			v.Mode = string(m)
		} else {
			v.Mode, v.New = string(suggestedMode(t)), true
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// SetMcpTool stores the mode of one tool, which also marks it as reviewed.
func (a *AssistantService) SetMcpTool(serverID, tool, mode string) error {
	m := assistant.ToolMode(mode)
	if m != assistant.ToolAllow && m != assistant.ToolAsk && m != assistant.ToolOff {
		return errors.New("mode must be allow, ask or off")
	}
	return a.update(func(c *assistant.Config) error {
		for i := range c.MCP {
			if c.MCP[i].ID == serverID {
				if c.MCP[i].Tools == nil {
					c.MCP[i].Tools = map[string]assistant.ToolMode{}
				}
				c.MCP[i].Tools[tool] = m
				return nil
			}
		}
		return errors.New("unknown MCP server")
	})
}

// ApproveNewTools reviews every new tool of a server with the suggested mode: allow for a read-only tool,
// ask for the others.
func (a *AssistantService) ApproveNewTools(serverID string) error {
	tools, err := a.TestMcpServer(serverID)
	if err != nil {
		return err
	}
	return a.update(func(c *assistant.Config) error {
		for i := range c.MCP {
			if c.MCP[i].ID != serverID {
				continue
			}
			if c.MCP[i].Tools == nil {
				c.MCP[i].Tools = map[string]assistant.ToolMode{}
			}
			for _, t := range tools {
				if t.New {
					c.MCP[i].Tools[t.Name] = assistant.ToolMode(t.Mode)
				}
			}
		}
		return nil
	})
}
