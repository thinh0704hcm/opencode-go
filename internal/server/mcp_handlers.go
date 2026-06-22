package server

import (
	"encoding/json"
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
	name := r.PathValue("name")
	if s.mcp == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp not configured")
		return
	}
	status, adapters := s.mcp.Connect(name)
	for _, a := range adapters {
		s.tools.Register(a)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":      name,
		"status":    status.Status,
		"error":     status.Error,
		"toolCount": status.ToolCount,
	})
}

// handleMCPDisconnect serves POST /mcp/{name}/disconnect.
func (s *Server) handleMCPDisconnect(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.mcp == nil {
		writeError(w, http.StatusServiceUnavailable, "mcp not configured")
		return
	}
	status := s.mcp.Disconnect(name)
	s.tools.Unregister(name + ":") // Unregister all tools with this prefix
	writeJSON(w, http.StatusOK, map[string]any{
		"name":   name,
		"status": status.Status,
	})
}

// handleMCPAdd serves POST /mcp.
func (s *Server) handleMCPAdd(w http.ResponseWriter, r *http.Request) {
    // Require JSON content type
    if !requireJSON(w, r) {
        return
    }
    if s.mcp == nil {
        writeError(w, http.StatusServiceUnavailable, "mcp not configured")
        return
    }
    var body map[string]any
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        writeError(w, http.StatusBadRequest, "invalid JSON")
        return
    }
    name, _ := body["name"].(string)
    cfg, _ := body["config"].(map[string]any)
    if name == "" {
        for k, v := range body {
            if k != "name" {
                name = k
                if c, ok := v.(map[string]any); ok {
                    cfg = c
                }
                break
            }
        }
    }
    if name == "" || cfg == nil {
        writeError(w, http.StatusBadRequest, "missing name or config")
        return
    }
    if err := s.mcp.Add(name, cfg); err != nil {
        writeError(w, http.StatusBadRequest, err.Error())
        return
    }
    status := s.mcp.Status()
    for _, st := range status {
        if st.Name == name {
            writeJSON(w, http.StatusOK, map[string]any{
                "name":      name,
                "status":    st.Status,
                "error":     st.Error,
                "toolCount": st.ToolCount,
            })
            return
        }
    }
    writeJSON(w, http.StatusOK, map[string]any{"name": name, "status": "added"})
}

// handleMCPAuthRemove serves DELETE /mcp/{name}/auth.
func (s *Server) handleMCPAuthRemove(w http.ResponseWriter, r *http.Request) {
	s.handleTUIOK(w, r)
}

// handleMCPAuth serves POST /mcp/{name}/auth.
func (s *Server) handleMCPAuth(w http.ResponseWriter, r *http.Request) {
	_ = r.PathValue("name")
	writeError(w, http.StatusNotImplemented, "MCP OAuth not implemented")
}

// handleMCPAuthAuthenticate serves POST /mcp/{name}/auth/authenticate.
func (s *Server) handleMCPAuthAuthenticate(w http.ResponseWriter, r *http.Request) {
	_ = r.PathValue("name")
	writeError(w, http.StatusNotImplemented, "MCP OAuth not implemented")
}

// handleMCPAuthCallback serves POST /mcp/{name}/auth/callback.
func (s *Server) handleMCPAuthCallback(w http.ResponseWriter, r *http.Request) {
	_ = r.PathValue("name")
	writeError(w, http.StatusNotImplemented, "MCP OAuth not implemented")
}
