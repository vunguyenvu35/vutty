package usecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"vutty/internal/domain"
	"vutty/internal/domain/assistant"
	"vutty/internal/domain/secretpolicy"
)

// ---- what the agent can reach through the gateway ----

// lazyServer starts a user's MCP server only when the agent first looks at it.
type lazyServer struct {
	connect func() (assistant.ToolServer, error)
	once    sync.Once
	srv     assistant.ToolServer
	err     error
}

func (l *lazyServer) open() (assistant.ToolServer, error) {
	l.once.Do(func() { l.srv, l.err = l.connect() })
	return l.srv, l.err
}

func (l *lazyServer) Tools() ([]assistant.Tool, error) {
	s, err := l.open()
	if err != nil {
		return nil, err
	}
	return s.Tools()
}

func (l *lazyServer) Call(name string, args json.RawMessage) (assistant.ToolResult, error) {
	s, err := l.open()
	if err != nil {
		return assistant.ToolResult{}, err
	}
	return s.Call(name, args)
}

func (l *lazyServer) Close() {
	if l.srv != nil {
		l.srv.Close()
	}
}

// terminalTools are the built-in tools: read the screen, propose a command, and (only when the user turned
// auto-run on) run one read-only command.
type terminalTools struct {
	a       *AssistantService
	run     *assistRun
	autoRun bool
}

func (t *terminalTools) Tools() ([]assistant.Tool, error) {
	ro := func(n, d, schema string) assistant.Tool {
		return assistant.Tool{Name: n, Description: d, InputSchema: json.RawMessage(schema), ReadOnly: true}
	}
	tools := []assistant.Tool{
		ro("terminal_read", "Read the latest text of the user's terminal. Secrets appear as *******.", `{"type":"object","properties":{"lines":{"type":"integer","description":"how many last lines (max 300)"}}}`),
		{Name: "terminal_propose_command", Description: "Show the user a command to approve. It is not run.", InputSchema: json.RawMessage(`{"type":"object","properties":{"cmd":{"type":"string"},"why":{"type":"string"}},"required":["cmd"]}`)},
	}
	if t.autoRun {
		tools = append(tools, assistant.Tool{Name: "terminal_run", Description: "Run one read-only command (ls, df, uptime, journalctl -n N, systemctl status X, ...) and read its output. Other commands are refused.", InputSchema: json.RawMessage(`{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}`)})
	}
	return tools, nil
}

func (t *terminalTools) Call(name string, args json.RawMessage) (assistant.ToolResult, error) {
	var in struct {
		Lines int    `json:"lines"`
		Cmd   string `json:"cmd"`
		Why   string `json:"why"`
	}
	_ = json.Unmarshal(args, &in)
	switch name {
	case "terminal_read":
		if in.Lines <= 0 || in.Lines > 300 {
			in.Lines = 100
		}
		text, _, _, err := t.a.screenText(t.run.sid, in.Lines, "")
		return assistant.ToolResult{Text: text}, err
	case "terminal_propose_command":
		cmd := strings.TrimSpace(in.Cmd)
		if cmd == "" || hasControl(cmd) {
			return assistant.ToolResult{Text: "the command must be one non-empty line", IsError: true}, nil
		}
		t.run.mu.Lock()
		t.run.proposed = append(t.run.proposed, proposal{cmd, in.Why})
		t.run.mu.Unlock()
		return assistant.ToolResult{Text: "The command was shown to the user, who decides whether to run it."}, nil
	case "terminal_run":
		out, err := t.a.runAndRead(t.run.sid, strings.TrimSpace(in.Cmd))
		return assistant.ToolResult{Text: out}, err
	}
	return assistant.ToolResult{Text: "unknown tool", IsError: true}, nil
}

func (t *terminalTools) Close() {}

// resolveSecrets decrypts the environment and header values of a server for one connection.
func (a *AssistantService) resolveSecrets(s assistant.MCPServer) (env, headers map[string]string, err error) {
	get := func(refs map[string]string) (map[string]string, error) {
		out := map[string]string{}
		for name, id := range refs {
			b, err := a.c.Vault.GetSecret(id)
			if err != nil {
				return nil, err
			}
			out[name] = string(b)
		}
		return out, nil
	}
	if env, err = get(s.EnvRefs); err != nil {
		return nil, nil, err
	}
	headers, err = get(s.HeaderRefs)
	return env, headers, err
}

func (a *AssistantService) connectMCP(s assistant.MCPServer) (assistant.ToolServer, error) {
	if a.c.MCP == nil {
		return nil, errors.New("MCP servers are not available in this build")
	}
	env, headers, err := a.resolveSecrets(s)
	if err != nil {
		return nil, err
	}
	return a.c.MCP.Connect(s, env, headers)
}

// gatewayParts builds what the gateway serves for one question and the rules it enforces.
func (a *AssistantService) gatewayParts(run *assistRun, g secretpolicy.Guard, provider string, info SessionState) (map[string]assistant.ToolServer, assistant.GatewayHooks) {
	cfg := a.config()
	autoRun := cfg.AutoRun && !info.Production
	servers := map[string]assistant.ToolServer{"": &terminalTools{a: a, run: run, autoRun: autoRun}}
	byslug := map[string]assistant.MCPServer{}
	hostID := strings.TrimPrefix(info.HostID, telnetPrefix)
	for _, s := range cfg.MCP {
		if s.InScope(hostID, provider) {
			s := s
			byslug[s.Slug()] = s
			servers[s.Slug()] = &lazyServer{connect: func() (assistant.ToolServer, error) { return a.connectMCP(s) }}
		}
	}
	hooks := assistant.GatewayHooks{
		Visible: func(server string, t assistant.Tool) bool {
			if server == "" {
				return true
			}
			s, ok := byslug[server]
			if !ok {
				return false
			}
			run.mu.Lock()
			run.toolRO[server+"/"+t.Name] = t.ReadOnly
			run.mu.Unlock()
			m := s.Tools[t.Name]
			return m == assistant.ToolAllow || m == assistant.ToolAsk // new tools (not listed) stay hidden until reviewed
		},
		Authorize: func(server, tool string, args json.RawMessage) error {
			return a.authorizeCall(run, g, byslug, server, tool, args)
		},
		Clean: func(text string) string {
			out, _ := g.Clean(text)
			if len(out) > maxResultBytes {
				out = out[:maxResultBytes] + "\n[truncated]"
			}
			return out
		},
		Observe: func(server, tool, state, note string) {
			label := tool
			if server != "" {
				label = byslug[server].Name + " · " + tool
			}
			note, _ = g.Clean(note)
			a.emit("assistant:step:"+run.id, map[string]any{"server": server, "tool": label, "state": state, "note": note})
			if state == "calling" || state == "blocked" {
				a.c.record(domain.ActAssistant, run.hostID, "", fmt.Sprintf("Tool %s: %s", label, state), state == "calling")
			}
		},
	}
	return servers, hooks
}

func (a *AssistantService) authorizeCall(run *assistRun, g secretpolicy.Guard, byslug map[string]assistant.MCPServer, server, tool string, args json.RawMessage) error {
	if len(args) > 16<<10 {
		return errors.New("arguments are too large")
	}
	if server == "" {
		switch tool {
		case "terminal_read", "terminal_propose_command":
			return nil
		case "terminal_run":
			var in struct {
				Cmd string `json:"cmd"`
			}
			_ = json.Unmarshal(args, &in)
			info, err := a.SessionInfo(run.sid)
			switch {
			case err != nil:
				return err
			case !a.config().AutoRun:
				return errors.New("running commands by itself is turned off")
			case info.Production || !info.Connected || info.Policy == string(assistant.PolicyOff):
				return errors.New("commands cannot run by themselves on this terminal")
			case !assistant.ReadOnlyAllowed(in.Cmd) || g.Contains(in.Cmd):
				return errors.New("only read-only commands from the allow-list run by themselves: use terminal_propose_command")
			}
			run.mu.Lock()
			run.calls++
			n := run.calls
			run.mu.Unlock()
			if n > 5 {
				return errors.New("too many commands in one request")
			}
			return nil
		}
		return errors.New("unknown tool")
	}
	s, ok := byslug[server]
	if !ok {
		return errors.New("unknown server")
	}
	mode := s.Tools[tool]
	if mode == "" || mode == assistant.ToolOff {
		return errors.New("this tool is turned off")
	}
	// a secret in the arguments blocks the call: masking it would silently change what the call means
	if g.Contains(string(args)) {
		return errors.New("arguments contain sensitive data")
	}
	run.mu.Lock()
	ro := run.toolRO[server+"/"+tool]
	run.mu.Unlock()
	if mode == assistant.ToolAllow && !ro && !s.Trusted {
		mode = assistant.ToolAsk // a server that is not trusted never runs a side-effecting tool unasked
	}
	if mode == assistant.ToolAsk {
		return a.askUser(run, g, s.Name, tool, args)
	}
	return nil
}

func (a *AssistantService) askUser(run *assistRun, g secretpolicy.Guard, server, tool string, args json.RawMessage) error {
	run.mu.Lock()
	callID := fmt.Sprintf("c%d", len(run.confirms)+1)
	ch := make(chan bool, 1)
	run.confirms[callID] = ch
	run.mu.Unlock()
	shown, _ := g.Clean(string(args))
	a.emit("assistant:confirm:"+run.id, map[string]any{"call": callID, "server": server, "tool": tool, "args": shown})
	select {
	case ok := <-ch:
		if !ok {
			return errors.New("the user declined this call")
		}
		return nil
	case <-time.After(2 * time.Minute):
		return errors.New("the user did not answer")
	case <-run.ctx.Done():
		return errors.New("cancelled")
	}
}
