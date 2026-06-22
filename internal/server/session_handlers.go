package server

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/session"
)

// validSessionID checks if a string looks like a valid session ID.
// Session IDs start with "ses_" followed by alphanumeric characters.
func validSessionID(id string) bool {
    if len(id) < 5 { // "ses_" + at least 1 char
        return false
    }
    if !strings.HasPrefix(id, "ses_") {
        return false
    }
    for _, c := range id[4:] {
        if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
            return false
        }
    }
    return true
}

// handleSessionList serves GET /session, returning a JSON array of all
// sessions (empty array on a fresh server, never null).
func (s *Server) handleSessionList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.List())
}

// handleSessionTodo serves GET /session/{id}/todo, returning the session's
// todo list (empty array when no todos exist).
func (s *Server) handleSessionTodo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validSessionID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid session ID format"})
		return
	}
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	todos, _ := s.store.GetTodos(id)
	if todos == nil {
		todos = []session.Todo{}
	}
	writeJSON(w, http.StatusOK, todos)
}

// handleSessionTodoUpdate serves POST/PATCH /session/{id}/todo and /api/session/{id}/todo
func (s *Server) handleSessionTodoUpdate(w http.ResponseWriter, r *http.Request) {
	// support both v1 and v2 path param names
	sessionID := r.PathValue("id")
	if sessionID == "" {
		sessionID = r.PathValue("sessionID")
	}
	if !validSessionID(sessionID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid session ID format"})
		return
	}
	if _, ok := s.store.GetSession(sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	var body struct {
		Todos []session.Todo `json:"todos"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	s.store.SetTodos(sessionID, body.Todos)
	s.bus.Publish(event.NewTodoUpdated(sessionID, body.Todos))
	writeJSON(w, http.StatusOK, body.Todos)
}

// handleSessionDiff serves GET /session/{id}/diff. It returns the current git
// diff stat (additions/deletions) for the workspace.
func (s *Server) handleSessionDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	diff, err := gitDiffStat(s.workdir)
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, diff)
}

// handleSessionSummarize serves POST /session/{id}/summarize. It re-derives the
// session title from the first user message text unconditionally and publishes
// session.updated.
func (s *Server) handleSessionSummarize(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validSessionID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid session ID format"})
		return
	}
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if msgs, ok := s.store.Messages(id); ok {
		for _, m := range msgs {
			if m.Info.Role == "user" {
				title := firstLine(partsText(m.Parts, "text"), 60)
				if title != "" {
					s.store.UpdateSessionTitle(id, title)
					if updated, ok := s.store.GetSession(id); ok {
						s.bus.Publish(event.NewSessionUpdated(id, updated))
					}
				}
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, true)
}

// handleSessionGet serves GET /session/{id}, returning the Session object.
func (s *Server) handleSessionGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validSessionID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid session ID format"})
		return
	}
	sess, ok := s.store.GetSession(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

// sessionUpdateRequest is the PATCH /session/{id} body.
type sessionUpdateRequest struct {
	Title *string `json:"title"`
}

// handleSessionUpdate serves PATCH /session/{id}, updating the title and
// publishing session.updated{sessionID, info}.
func (s *Server) handleSessionUpdate(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if !validSessionID(id) {
        writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid session ID format"})
        return
    }
    if _, ok := s.store.GetSession(id); !ok {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }

    if !requireJSON(w, r) {
        return
    }

    var req sessionUpdateRequest
    if !decodeStrictBody(w, r, &req, false) {
        // decodeStrictBody already wrote an error response.
        return
    }

    if req.Title != nil && strings.TrimSpace(*req.Title) == "" {
        writeError(w, http.StatusBadRequest, "invalid title")
        return
    }

    // req.Title is nil when omitted; UpdateTitle applies only non-nil.
    sess, ok := s.store.UpdateTitle(id, req.Title)
    if !ok {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }
    s.store.PersistSession(id)
    s.bus.Publish(event.NewSessionUpdated(id, sess))
    writeJSON(w, http.StatusOK, sess)
}

// handleSessionDelete serves DELETE /session/{id}, removing the session and its
// messages and publishing session.deleted{info}. Returns the bool true.
func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validSessionID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid session ID format"})
		return
	}
	sess, ok := s.store.GetSession(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	s.cancelSession(id) // abort any in-flight generation
	// Remove queue entry to prevent memory leak; cancel already signalled abort.
	s.sesMu.Lock()
	delete(s.sesQueue, id)
	s.sesMu.Unlock()
	if !s.store.Delete(id) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	s.store.RemovePersisted(id)
	s.bus.Publish(event.NewSessionDeleted(id, sess))
	writeJSON(w, http.StatusOK, true)
}

// handleSessionChildren serves GET /session/{id}/children, returning child
// sessions created by delegate/task tool calls with this session as parent.
func (s *Server) handleSessionChildren(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validSessionID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid session ID format"})
		return
	}
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	children := s.store.Children(id)
	if children == nil {
		children = []session.Session{}
	}
	writeJSON(w, http.StatusOK, children)
}

// handleExperimentalSessionBackground serves POST /experimental/session/{id}/background.
// Real opencode detaches synchronous subagents to the background; we return 200
// so the TUI doesn't see a 404 and can continue interacting with the parent.
func (s *Server) handleExperimentalSessionBackground(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": true})
}

// handleSessionAbort serves POST /session/{id}/abort. It drains any pending
// generation tasks and cancels the currently in-flight turn via the per-session
// cancel registry. The processQueue loop will naturally emit idle events when
// it finds the queue empty.
func (s *Server) handleSessionAbort(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Drain queue first so no more items start after the cancel.
	s.sesMu.Lock()
	work := s.sesQueue[id]
	if work == nil || !work.running {
		s.sesMu.Unlock()
		// Already idle — emit idle confirmation so SSE-watching clients unblock.
		s.bus.Publish(event.NewSessionStatus(id, map[string]string{"type": "idle"}))
		s.bus.Publish(event.NewSessionIdle(id))
s.bus.Publish(event.NewSessionStatus(id, map[string]string{"type": "idle"}))
        s.bus.Publish(event.NewSessionIdle(id))
        writeJSON(w, http.StatusOK, true)
		return
	}
	work.queue = work.queue[:0]
	work.draining = true
	s.sesMu.Unlock()

	s.cancelSession(id)
	writeJSON(w, http.StatusOK, true)
}

// handleGetMessage serves GET /session/{id}/message/{messageID}, returning the
// single {info, parts} for that message (404 if not found).
func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	messageID := r.PathValue("messageID")
	mwp, ok := s.store.GetMessage(id, messageID)
	if !ok {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	writeJSON(w, http.StatusOK, mwp)
}

// handleSessionRevert stashes uncommitted changes for the session's working directory.
func (s *Server) handleSessionRevert(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    sess, ok := s.store.GetSession(id)
    if !ok {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }
    if s.sessionBusy(id) {
        writeJSON(w, http.StatusConflict, map[string]any{"_tag":"ConflictError","message":"session is busy","resource":"session"})
        return
    }
    // Validate JSON content type and payload.
    if !requireJSON(w, r) {
        return
    }
    var req struct {
        MessageID string `json:"messageID"`
        PartID    string `json:"partID,omitempty"`
    }
    if !decodeStrictBody(w, r, &req, false) {
        return
    }
    if strings.TrimSpace(req.MessageID) == "" || !strings.HasPrefix(req.MessageID, "msg") {
        writeJSON(w, http.StatusBadRequest, map[string]any{"error": "messageID must be a non-empty string starting with 'msg'"})
        return
    }
    dir := sess.Directory
    if dir == "" {
        dir = s.workdir
    }
    out, err := exec.CommandContext(r.Context(), "git", "-C", dir, "stash", "push", "-u", "--message", "opencode-revert-"+id).CombinedOutput()
    if err != nil {
        writeError(w, http.StatusInternalServerError, strings.TrimSpace(string(out)))
        return
    }
    // partID is currently unused; validation only.
    _ = req.PartID
s.bus.Publish(event.NewSessionStatus(id, map[string]string{"type": "idle"}))
        s.bus.Publish(event.NewSessionIdle(id))
        writeJSON(w, http.StatusOK, true)
}

// handleSessionUnrevert pops the stash created by revert.
func (s *Server) handleSessionUnrevert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, ok := s.store.GetSession(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	dir := sess.Directory
	if dir == "" {
		dir = s.workdir
	}
	out, err := exec.CommandContext(r.Context(), "git", "-C", dir, "stash", "pop").CombinedOutput()
	if err != nil {
		writeError(w, http.StatusInternalServerError, strings.TrimSpace(string(out)))
		return
	}
	writeJSON(w, http.StatusOK, true)
}

// handleSessionNoop acknowledges SDK/TUI session actions that are not implemented
// by opencode-go yet but should not break the 1.17.x client boot flow.
func (s *Server) handleSessionNoop(w http.ResponseWriter, r *http.Request) {
    // Require JSON content type
    if !requireJSON(w, r) {
        return
    }
    // Decode strict body expecting required fields
    var body struct {
        MessageID  string `json:"messageID"`
        ModelID    string `json:"modelID"`
        ProviderID string `json:"providerID"`
    }
    if !decodeStrictBody(w, r, &body, false) {
        return
    }
    // Validate all non-empty
    if strings.TrimSpace(body.MessageID) == "" || strings.TrimSpace(body.ModelID) == "" || strings.TrimSpace(body.ProviderID) == "" {
        writeError(w, http.StatusBadRequest, "missing required fields")
        return
    }
    id := r.PathValue("id")
    if _, ok := s.store.GetSession(id); !ok {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }
    // Not implemented yet
    writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "not implemented"})
}

// handleSessionFork currently returns a new child session placeholder.
func (s *Server) handleSessionFork(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    parent, ok := s.store.GetSession(id)
    if !ok {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }
    // If body present, decode strict (optional messageID)
    if r.ContentLength > 0 {
        var body struct {
            MessageID string `json:"messageID,omitempty"`
        }
        if !decodeStrictBody(w, r, &body, false) {
            return
        }
        // Currently messageID not used; placeholder for future.
        _ = body.MessageID
    }

    child := s.store.CreateSession(id, parent.Title+" (fork)", parent.Directory)
    // Copy messages from parent into child
    if msgs, ok := s.store.Messages(id); ok {
        for _, m := range msgs {
            s.store.CopyMessage(child.ID, m)
        }
    }
    s.store.PersistSession(child.ID)
    writeJSON(w, http.StatusOK, child)
}

func (s *Server) handleSessionShare(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": "", "share": false})
}

func (s *Server) handleSessionUnshare(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, true)
}

func (s *Server) handleSessionCommand(w http.ResponseWriter, r *http.Request) {
    // Validate JSON payload and required fields.
    if !requireJSON(w, r) {
        return
    }
    var cmd struct {
        Command   string `json:"command"`
        Arguments string `json:"arguments"`
    }
    if !decodeStrictBody(w, r, &cmd, false) {
        return
    }
    if strings.TrimSpace(cmd.Command) == "" || strings.TrimSpace(cmd.Arguments) == "" {
        writeJSON(w, http.StatusBadRequest, map[string]any{"error": "command and arguments must be non-empty"})
        return
    }
    // Existing behavior: forward to prompt handler.
    s.handlePrompt(w, r)
    // Publish command executed event after successful prompt handling.
    id := r.PathValue("id")
    s.bus.Publish(event.NewCommandExecuted(cmd.Command, id, cmd.Arguments, ""))
}
