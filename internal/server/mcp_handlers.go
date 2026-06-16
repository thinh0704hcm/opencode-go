package server

import (
	"net/http"
)

// handleMCP serves GET /mcp. It returns the MCP manager's real per-server
// status (name, status, error, toolCount). When no manager is configured it
// returns an empty array. REDACTION: command/url/environment are never emitted.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.mcp.Status())
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
	name := r.PathValue("name")
	s.tools.Unregister(name + "_")
	writeJSON(w, http.StatusOK, map[string]any{
		"name": name,
	})
}

// handleMCPAdd serves POST /mcp.
func (s *Server) handleMCPAdd(w http.ResponseWriter, r *http.Request) {
	s.handleTUIOK(w, r)
}

// handleMCPAuthRemove serves DELETE /mcp/{name}/auth.
func (s *Server) handleMCPAuthRemove(w http.ResponseWriter, r *http.Request) {
	s.handleTUIOK(w, r)
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
