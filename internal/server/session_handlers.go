package server

import (
	"net/http"

	"github.com/opencode-go/opencode-go/internal/event"
)

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
	if !s.store.Delete(id) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
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

// handleSessionAbort serves POST /session/{id}/abort. M1's generation worker has
// no per-session cancel registry (it runs on context.Background()), so this is a
// no-op that publishes a final session.idle{sessionID} and returns true.
func (s *Server) handleSessionAbort(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
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
