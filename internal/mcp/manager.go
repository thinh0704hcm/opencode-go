package mcp

import (
	"fmt"
	"log"
	"sync"

	"github.com/opencode-go/opencode-go/internal/tool"
)

// ServerStatus reports the connection state of one configured MCP server.
type ServerStatus struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // "connected" | "disconnected" | "error" | "unsupported"
	Error     string `json:"error,omitempty"`
	ToolCount int    `json:"toolCount"`
}

// Manager owns the lifecycle of all configured MCP client connections and
// exposes their tools as tool.Tool adapters for the agent registry.
type Manager struct {
	mu       sync.Mutex
	clients  []*Client
	statuses []ServerStatus
	adapters []tool.Tool
}

// NewManager parses an opencode "mcp" config section (map of serverName ->
// config) and connects every enabled LOCAL server. Connection failures are
// recorded as status "error" and do not abort the others. Remote servers are
// recorded "unsupported". The returned Manager is ready; call Adapters() to get
// the tools and Shutdown() to release processes. A nil/empty section yields an
// empty Manager.
func NewManager(section map[string]any) *Manager {
	m := &Manager{}
	for name, rawCfg := range section {
		cfg, ok := rawCfg.(map[string]any)
		if !ok {
			m.statuses = append(m.statuses, ServerStatus{Name: name, Status: "error", Error: "invalid config shape"})
			continue
		}
		if !mcpEnabled(cfg) {
			m.statuses = append(m.statuses, ServerStatus{Name: name, Status: "disconnected"})
			continue
		}
		typ, _ := cfg["type"].(string)
		if typ == "remote" {
			m.statuses = append(m.statuses, ServerStatus{Name: name, Status: "unsupported", Error: "remote transport not implemented"})
			continue
		}
		argv := stringSlice(cfg["command"])
		if len(argv) == 0 {
			m.statuses = append(m.statuses, ServerStatus{Name: name, Status: "error", Error: "missing command"})
			continue
		}
		env := envSlice(cfg["environment"])
		client, err := NewClient(name, argv, env)
		if err != nil {
			log.Printf("mcp: connect %q failed: %v", name, err)
			m.statuses = append(m.statuses, ServerStatus{Name: name, Status: "error", Error: err.Error()})
			continue
		}
		defs, err := client.ListTools()
		if err != nil {
			log.Printf("mcp: tools/list %q failed: %v", name, err)
			client.Close()
			m.statuses = append(m.statuses, ServerStatus{Name: name, Status: "error", Error: err.Error()})
			continue
		}
		m.clients = append(m.clients, client)
		m.adapters = append(m.adapters, NewToolAdapters(client, defs)...)
		m.statuses = append(m.statuses, ServerStatus{Name: name, Status: "connected", ToolCount: len(defs)})
		log.Printf("mcp: connected %q with %d tools", name, len(defs))
	}
	return m
}

// Adapters returns the tool.Tool adapters for all connected MCP servers.
func (m *Manager) Adapters() []tool.Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]tool.Tool, len(m.adapters))
	copy(out, m.adapters)
	return out
}

// Status returns a snapshot of every configured server's connection state.
func (m *Manager) Status() []ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ServerStatus, len(m.statuses))
	copy(out, m.statuses)
	return out
}

// Shutdown closes all client connections (terminating their processes).
func (m *Manager) Shutdown() {
	m.mu.Lock()
	clients := m.clients
	m.clients = nil
	m.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
}

// mcpEnabled reports whether a server config is enabled (default true when the
// key is absent).
func mcpEnabled(cfg map[string]any) bool {
	if v, ok := cfg["enabled"]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return true
}

// stringSlice coerces a config value (an []any of strings, or a single string)
// into a []string.
func stringSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	case string:
		if t != "" {
			return []string{t}
		}
	}
	return nil
}

// envSlice coerces an environment map (map[string]any of string values) into
// KEY=VALUE strings.
func envSlice(v any) []string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out = append(out, fmt.Sprintf("%s=%s", k, s))
		}
	}
	return out
}
