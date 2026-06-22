//go:build opencode_wip

package server

import (
	"encoding/json"
	"net/http"
)

// worktreeCreateRequest is POST /experimental/worktree body.
type worktreeCreateRequest struct {
	Path string `json:"path"`
}

// worktreeDeleteRequest is DELETE /experimental/worktree body.
type worktreeDeleteRequest struct {
	Path string `json:"path"`
}

// worktreeAssignRequest is POST /experimental/worktree for assignment (sessionID optional).
type worktreeAssignRequest struct {
	SessionID string `json:"sessionID,omitempty"`
	Path      string `json:"path"`
}

// worktreeResetRequest is POST /experimental/worktree/reset body.
type worktreeResetRequest struct {
	SessionID string `json:"sessionID"`
}

// handleExperimentalWorktreeCreate serves POST /experimental/worktree (create or assign).
func (s *Server) handleExperimentalWorktreeCreate(w http.ResponseWriter, r *http.Request) {
	// Decode request. May contain just path (create) or sessionID+path (assign).
	var req worktreeAssignRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		// Fallback to create request format.
		var cr worktreeCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&cr); err != nil || cr.Path == "" {
			writeError(w, http.StatusBadRequest, "path required")
			return
		}
		wt, err := s.worktrees.Add(cr.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, wt)
		return
	}
	// If SessionID provided, treat as assign.
	if req.SessionID != "" {
		if err := s.worktrees.Assign(s.store, req.SessionID, req.Path); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "assigned"})
		return
	}
	// Otherwise pure create.
	wt, err := s.worktrees.Add(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wt)
}

// handleExperimentalWorktreeDelete serves DELETE /experimental/worktree.
func (s *Server) handleExperimentalWorktreeDelete(w http.ResponseWriter, r *http.Request) {
	var req worktreeDeleteRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}
	if err := s.worktrees.Delete(req.Path, s.store); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleExperimentalWorktreeReset serves POST /experimental/worktree/reset.
func (s *Server) handleExperimentalWorktreeReset(w http.ResponseWriter, r *http.Request) {
	var req worktreeResetRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "sessionID required")
		return
	}
	if err := s.worktrees.Reset(s.store, req.SessionID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
