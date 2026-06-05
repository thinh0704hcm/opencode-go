package server

import (
	"context"
	"io"
	"net/http"
	"time"
)

// tuiControlNextTimeout bounds the GET /tui/control/next long-poll. On timeout
// the handler returns 200 with a JSON null so the TUI can immediately re-poll.
const tuiControlNextTimeout = 25 * time.Second

// handleTUIControlNext serves GET /tui/control/next as a long-poll. It blocks up
// to tuiControlNextTimeout, returning 200 with `null` when the timer fires or
// the client disconnects. M2 has no queued control messages, so the only exit
// paths are timeout and client cancellation; neither panics.
func (s *Server) handleTUIControlNext(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), tuiControlNextTimeout)
	defer cancel()

	<-ctx.Done()
	writeJSON(w, http.StatusOK, nil)
}

// handleLog serves POST /log. The body is drained and discarded, a debug line is
// emitted, and an empty JSON object is returned with 200.
func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		_, _ = io.Copy(io.Discard, r.Body)
	}
	s.logger.Debug("tui log received")
	writeJSON(w, http.StatusOK, map[string]any{})
}
