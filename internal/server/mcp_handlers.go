package server

import (
	"net/http"

	"github.com/opencode-go/opencode-go/internal/config"
)

// mcpStatus is one MCP server entry in GET /mcp. Only the name (map key) and a
// derived status are exposed; the command/url/environment are never emitted so
// no secrets leak. Error is optional and omitted when empty.
type mcpStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// handleMCP serves GET /mcp. It returns a map keyed by configured MCP server
// name with a derived status, sourced from config.Load(dir).Raw["mcp"]. Since
// this build does NOT spawn/connect MCP clients, every configured server is
// reported as "disconnected". When no mcp config exists, it returns {} (an
// empty object, never null). REDACTION: only name+status are emitted.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	out := map[string]mcpStatus{}

	cfg := config.Load(directoryParam(r))
	if mcpRaw, ok := cfg.Raw["mcp"].(map[string]any); ok {
		for name := range mcpRaw {
			out[name] = mcpStatus{Status: "disconnected"}
		}
	}

	writeJSON(w, http.StatusOK, out)
}

// handleMCPConnect serves POST /mcp/{name}/connect. The real MCP client is not
// implemented, so it returns a typed 200 body rather than a 404/500 so clients
// do not error.
func (s *Server) handleMCPConnect(w http.ResponseWriter, r *http.Request) {
	_ = r.PathValue("name")
	writeJSON(w, http.StatusOK, map[string]any{
		"connected": false,
		"error":     "mcp client not implemented",
	})
}

// handleMCPDisconnect serves POST /mcp/{name}/disconnect.
func (s *Server) handleMCPDisconnect(w http.ResponseWriter, r *http.Request) {
	_ = r.PathValue("name")
	writeJSON(w, http.StatusOK, map[string]any{
		"disconnected": true,
	})
}

// handleMCPAuth serves POST /mcp/{name}/auth.
func (s *Server) handleMCPAuth(w http.ResponseWriter, r *http.Request) {
	_ = r.PathValue("name")
	writeJSON(w, http.StatusOK, map[string]any{
		"error": "not implemented",
	})
}

// handleMCPAuthAuthenticate serves POST /mcp/{name}/auth/authenticate.
func (s *Server) handleMCPAuthAuthenticate(w http.ResponseWriter, r *http.Request) {
	_ = r.PathValue("name")
	writeJSON(w, http.StatusOK, map[string]any{
		"error": "not implemented",
	})
}

// handleMCPAuthCallback serves POST /mcp/{name}/auth/callback.
func (s *Server) handleMCPAuthCallback(w http.ResponseWriter, r *http.Request) {
	_ = r.PathValue("name")
	writeJSON(w, http.StatusOK, map[string]any{
		"error": "not implemented",
	})
}
