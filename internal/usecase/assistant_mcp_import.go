package usecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"vutty/internal/domain/assistant"
)

// ---- importing from the CLIs' own configuration ----

type McpImportItem struct {
	Ref     string   `json:"ref"`
	Source  string   `json:"source"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	URL     string   `json:"url"`
	Env     []string `json:"env"`     // names only
	Headers []string `json:"headers"` // names only
}

type importedServer struct {
	item    McpImportItem
	env     map[string]string
	headers map[string]string
}

type cliMCP struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url"`
	HTTPURL string            `json:"httpUrl"`
	Headers map[string]string `json:"headers"`
}

// readImportable reads the CLIs' MCP lists. It is called only when the user opens the import dialog or
// presses Import, never in the background.
func readImportable() []importedServer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []importedServer
	collect := func(source string, servers map[string]cliMCP) {
		for name, s := range servers {
			is := importedServer{item: McpImportItem{Ref: source + ":" + name, Source: source, Name: name, Command: s.Command, Args: orEmpty(s.Args)}, env: s.Env, headers: s.Headers}
			url := s.URL
			if s.HTTPURL != "" {
				url = s.HTTPURL
			}
			switch {
			case s.Command != "":
				is.item.Type = assistant.MCPStdio
			case url != "" && s.Type != "sse":
				is.item.Type, is.item.URL = assistant.MCPHTTP, url
			default:
				continue // an SSE-only server is not supported
			}
			is.item.Env, is.item.Headers = sortedKeys(s.Env), sortedKeys(s.Headers)
			out = append(out, is)
		}
	}
	if b, err := os.ReadFile(filepath.Join(home, ".claude.json")); err == nil {
		var f struct {
			MCP      map[string]cliMCP `json:"mcpServers"`
			Projects map[string]struct {
				MCP map[string]cliMCP `json:"mcpServers"`
			} `json:"projects"`
		}
		if json.Unmarshal(b, &f) == nil {
			collect("claude", f.MCP)
		}
	}
	if b, err := os.ReadFile(filepath.Join(home, ".gemini", "settings.json")); err == nil {
		var f struct {
			MCP map[string]cliMCP `json:"mcpServers"`
		}
		if json.Unmarshal(b, &f) == nil {
			collect("gemini", f.MCP)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].item.Ref < out[j].item.Ref })
	return out
}

func (a *AssistantService) ImportableMcpServers() ([]McpImportItem, error) {
	out := []McpImportItem{}
	for _, s := range readImportable() {
		out = append(out, s.item)
	}
	return out, nil
}

// ImportMcpServer copies one server; its secret values go to the vault. It starts off and untrusted.
func (a *AssistantService) ImportMcpServer(ref string) (McpServerView, error) {
	for _, s := range readImportable() {
		if s.item.Ref != ref {
			continue
		}
		name := s.item.Name
		for n := 2; ; n++ {
			clash := false
			for _, x := range a.config().MCP {
				clash = clash || x.Slug() == (assistant.MCPServer{Name: name}).Slug()
			}
			if !clash {
				break
			}
			name = fmt.Sprintf("%s %d", s.item.Name, n)
		}
		return a.SaveMcpServer(McpServerInput{Name: name, Type: s.item.Type, Command: s.item.Command, Args: s.item.Args, URL: s.item.URL,
			Env: s.env, Headers: s.headers})
	}
	return McpServerView{}, errors.New("that server is no longer in the CLI's configuration")
}
