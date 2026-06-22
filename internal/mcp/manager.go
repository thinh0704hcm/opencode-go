package mcp

import (
	"fmt"
	"log"
	"sync"
	"time"

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
    configs  map[string]map[string]any
    clients  map[string]MCPClient
    statuses map[string]ServerStatus
    adapters map[string][]tool.Tool
    onToolsChanged func(server string)
}

// NewManager parses an opencode "mcp" config section (map of serverName ->
// config) and connects every enabled LOCAL server. Connection failures are
// recorded as status "error" and do not abort the others. Remote servers are
// recorded "unsupported". The returned Manager is ready; call Adapters() to get
// the tools and Shutdown() to release processes. A nil/empty section yields an
// empty Manager.
func NewManager(section map[string]any) *Manager {
    // Initialize maps.
    m := &Manager{
        configs:  make(map[string]map[string]any),
        clients:  make(map[string]MCPClient),
        statuses: make(map[string]ServerStatus),
        adapters: make(map[string][]tool.Tool),
    }
    for name, rawCfg := range section {
        cfg, ok := rawCfg.(map[string]any)
        if !ok {
            m.statuses[name] = ServerStatus{Name: name, Status: "error", Error: "invalid config shape"}
            continue
        }
        // Store config for later runtime ops.
        m.configs[name] = cfg
        if !mcpEnabled(cfg) {
            m.statuses[name] = ServerStatus{Name: name, Status: "disconnected"}
            continue
        }
        // Attempt connection for enabled local servers.
        if st, ads, err := m.connectLocked(name); err == nil {
            m.statuses[name] = st
            m.adapters[name] = ads
            log.Printf("mcp: connected %q with %d tools", name, st.ToolCount)
        } else {
            // connectLocked already logged errors and set status.
            // Ensure adapters entry cleared on failure.
            m.adapters[name] = nil
        }
    }
    return m
}

// SetToolsChangedCallback registers a callback for tool list changes.
func (m *Manager) SetToolsChangedCallback(fn func(server string)) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.onToolsChanged = fn
}

// AdaptersFor returns a copy of the tool adapters for a specific server.
func (m *Manager) AdaptersFor(name string) []tool.Tool {
    m.mu.Lock()
    defer m.mu.Unlock()
    ads := m.adapters[name]
    out := make([]tool.Tool, len(ads))
    copy(out, ads)
    return out
}

// connectLocked performs the actual client creation and tool discovery for a given server.
// It assumes the caller holds the manager lock.
func (m *Manager) connectLocked(name string) (ServerStatus, []tool.Tool, error) {
    cfg := m.configs[name]
    typ, _ := cfg["type"].(string)
    if typ == "remote" {
        url, _ := cfg["url"].(string)
        if url == "" {
            st := ServerStatus{Name: name, Status: "error", Error: "missing url for remote server"}
            m.statuses[name] = st
            return st, nil, fmt.Errorf("missing url")
        }
        headers := map[string]string{}
        if h, ok := cfg["headers"].(map[string]any); ok {
            for k, v := range h {
                if s, ok := v.(string); ok {
                    headers[k] = s
                }
            }
        }
        timeout := 30 * time.Second
        if t, ok := cfg["timeout"].(float64); ok && t > 0 {
            timeout = time.Duration(t) * time.Second
        }
        client := NewHTTPClient(name, url, headers, timeout)
        if err := client.Initialize(); err != nil {
            st := ServerStatus{Name: name, Status: "error", Error: err.Error()}
            m.statuses[name] = st
            return st, nil, err
        }
        defs, err := client.ListTools()
        if err != nil {
            _ = client.Close()
            st := ServerStatus{Name: name, Status: "error", Error: err.Error()}
            m.statuses[name] = st
            return st, nil, err
        }
        m.clients[name] = client
        ads := NewToolAdapters(client, defs)
        st := ServerStatus{Name: name, Status: "connected", ToolCount: len(defs)}
        m.statuses[name] = st
        return st, ads, nil
    }
    argv := stringSlice(cfg["command"])
    if len(argv) == 0 {
        st := ServerStatus{Name: name, Status: "error", Error: "missing command"}
        m.statuses[name] = st
        return st, nil, fmt.Errorf("missing command")
    }
    env := envSlice(cfg["environment"])
    client, err := NewStdioClient(name, argv, env)
    if err != nil {
        log.Printf("mcp: connect %q failed: %v", name, err)
        st := ServerStatus{Name: name, Status: "error", Error: err.Error()}
        m.statuses[name] = st
        return st, nil, err
    }
    defs, err := client.ListTools()
    if err != nil {
        log.Printf("mcp: tools/list %q failed: %v", name, err)
        _ = client.Close()
        st := ServerStatus{Name: name, Status: "error", Error: err.Error()}
        m.statuses[name] = st
        return st, nil, err
    }
    // Store successful connection.
    m.clients[name] = client
    ads := NewToolAdapters(client, defs)
    st := ServerStatus{Name: name, Status: "connected", ToolCount: len(defs)}
    m.statuses[name] = st
    // Register notification callbacks.
    client.OnToolsChanged(func() { m.refreshTools(name, client) })
    client.OnClose(func(err error) { m.markClosed(name, client, err) })
    return st, ads, nil
}

// Add stores a server config and optionally connects if enabled.
func (m *Manager) Add(name string, cfg map[string]any) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.configs[name] = cfg
    if !mcpEnabled(cfg) {
        m.statuses[name] = ServerStatus{Name: name, Status: "disconnected"}
        return nil
    }
    // Attempt connection.
    st, ads, err := m.connectLocked(name)
    m.statuses[name] = st
    if err == nil {
        m.adapters[name] = ads
    } else {
        m.adapters[name] = nil
    }
    return err
}

// Connect (re)connects a server by name, returning its status and adapters.
func (m *Manager) Connect(name string) (ServerStatus, []tool.Tool) {
    // Acquire lock to fetch config and possibly existing client.
    m.mu.Lock()
	_, ok := m.configs[name]
    if !ok {
        st := ServerStatus{Name: name, Status: "error", Error: "config not found"}
        m.statuses[name] = st
        m.mu.Unlock()
        return st, nil
    }
    var oldClient MCPClient
    if c, exists := m.clients[name]; exists {
        oldClient = c
        delete(m.clients, name)
        delete(m.adapters, name)
    }
    m.mu.Unlock()

    // Close old client outside lock to avoid blocking.
    if oldClient != nil {
        _ = oldClient.Close()
    }

    // Re‑lock for establishing new connection.
    m.mu.Lock()
    st, ads, err := m.connectLocked(name)
    m.statuses[name] = st
    if err == nil {
        m.adapters[name] = ads
    } else {
        m.adapters[name] = nil
    }
    m.mu.Unlock()
    return st, ads
}

// Disconnect closes a server client and marks it disconnected.
func (m *Manager) Disconnect(name string) ServerStatus {
    m.mu.Lock()
    defer m.mu.Unlock()
    if c, ok := m.clients[name]; ok {
        _ = c.Close()
        delete(m.clients, name)
    }
    delete(m.adapters, name)
    st := ServerStatus{Name: name, Status: "disconnected"}
    m.statuses[name] = st
    return st
}

// Adapters returns the tool.Tool adapters for all connected MCP servers.
func (m *Manager) Adapters() []tool.Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []tool.Tool
	for _, list := range m.adapters {
		out = append(out, list...)
	}
	return out
}

// Status returns a snapshot of every configured server's connection state.
func (m *Manager) Status() []ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ServerStatus, 0, len(m.statuses))
	for _, st := range m.statuses {
		out = append(out, st)
	}
	return out
}

// Shutdown closes all client connections (terminating their processes).
func (m *Manager) Shutdown() {
	m.mu.Lock()
	clientsMap := m.clients
	m.clients = nil
	m.adapters = nil
	m.statuses = nil
	m.configs = nil
	m.mu.Unlock()
	for _, c := range clientsMap {
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

// refreshTools re-fetches tools from a server after a tools/list_changed notification.
func (m *Manager) refreshTools(name string, client MCPClient) {
	m.mu.Lock()
	if m.clients[name] != client {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	defs, err := client.ListTools()
	if err != nil {
		log.Printf("mcp: refresh tools %q failed: %v", name, err)
		return
	}

	m.mu.Lock()
	if m.clients[name] != client {
		m.mu.Unlock()
		return
	}
	m.adapters[name] = NewToolAdapters(client, defs)
	m.statuses[name] = ServerStatus{Name: name, Status: "connected", ToolCount: len(defs)}
	cb := m.onToolsChanged
	m.mu.Unlock()

	log.Printf("mcp: %q tools refreshed (%d tools)", name, len(defs))
	if cb != nil {
		cb(name)
	}
}

// markClosed handles a client connection closing unexpectedly.
func (m *Manager) markClosed(name string, client MCPClient, err error) {
	m.mu.Lock()
	if m.clients[name] != client {
		m.mu.Unlock()
		return
	}
	delete(m.clients, name)
	delete(m.adapters, name)
	m.statuses[name] = ServerStatus{Name: name, Status: "failed", Error: "Connection closed"}
	cb := m.onToolsChanged
	m.mu.Unlock()

	log.Printf("mcp: %q connection closed: %v", name, err)
	if cb != nil {
		cb(name)
	}
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
