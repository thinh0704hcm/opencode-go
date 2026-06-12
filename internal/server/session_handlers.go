package server

import (
	"net/http"

	"github.com/opencode-go/opencode-go/internal/event"
)

// handleSessionList serves GET /session, returning a JSON array of all
// sessions (empty array on a fresh server, never null).
func (s *Server) handleSessionList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.List())
}

// handleSessionTodo serves GET /session/{id}/todo. No todos feature exists, so
// it returns an empty JSON array.
func (s *Server) handleSessionTodo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []interface{}{})
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
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var req sessionUpdateRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// req.Title is nil when the field is omitted; UpdateTitle leaves the title
	// unchanged in that case and only applies a non-nil value.
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
	sess, ok := s.store.GetSession(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	s.cancelSession(id) // abort any in-flight generation
	if !s.store.Delete(id) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	s.store.RemovePersisted(id)
	s.bus.Publish(event.NewSessionDeleted(sess))
	writeJSON(w, http.StatusOK, true)
}

// handleSessionChildren serves GET /session/{id}/children. No child sessions
// exist yet, so it returns an empty array (404 if the session is unknown).
func (s *Server) handleSessionChildren(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, []interface{}{})
}

// handleSessionAbort serves POST /session/{id}/abort. It cancels the in-flight
// generation turn (if any) via the per-session cancel registry and publishes
// session.status{idle} + session.idle{sessionID} so the TUI clears its busy
// state, then returns true.
func (s *Server) handleSessionAbort(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	s.cancelSession(id)
	s.bus.Publish(event.NewSessionStatus(id, map[string]string{"type": "idle"}))
	s.bus.Publish(event.NewSessionIdle(id))
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

// handleSessionNoop acknowledges SDK/TUI session actions that are not implemented
// by opencode-go yet but should not break the 1.17.x client boot flow.
func (s *Server) handleSessionNoop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, true)
}

// handleSessionFork currently returns a new child session placeholder.
func (s *Server) handleSessionFork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	sess := s.store.CreateSession(id, "", directoryParam(r))
	s.store.PersistSession(sess.ID)
	writeJSON(w, http.StatusOK, sess)
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
	s.handlePrompt(w, r)
}
