package server

import (
	"net/http"
	"time"

	"github.com/opencode-go/opencode-go/internal/event"
)

// handleGlobalEvent serves GET /global/event with the {directory, payload}
// envelope. Accepts optional ?directory=<cwd>, tolerates its absence, never
// uses it as a filter (architecture §7.2/B3).
func (s *Server) handleGlobalEvent(w http.ResponseWriter, r *http.Request) {
	s.serveSSE(w, r, event.KindGlobalEvent, directoryOf(r))
}

// handleBareEvent serves GET /event with a bare Event payload.
func (s *Server) handleBareEvent(w http.ResponseWriter, r *http.Request) {
	s.serveSSE(w, r, event.KindEvent, directoryOf(r))
}

// serveSSE subscribes to the bus and streams events. It IMMEDIATELY writes a
// server.connected event before entering the loop (architecture §2.3).
func (s *Server) serveSSE(w http.ResponseWriter, r *http.Request, kind event.EndpointKind, dir string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sub, cancel := s.bus.Subscribe()
	defer cancel()

	// Immediate server.connected handshake, sent per-subscriber synchronously
	// (never via the bus, so it is never dropped or delayed).
	if !s.writeEvent(w, flusher, event.NewServerConnected(), kind, dir) {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Events():
			if !ok {
				return
			}
			if !s.writeEvent(w, flusher, ev, kind, dir) {
				return
			}
		case <-ticker.C:
			if err := event.WriteHeartbeat(w, flusher); err != nil {
				return
			}
		}
	}
}

// writeEvent wraps and writes one event frame; returns false on write error.
func (s *Server) writeEvent(w http.ResponseWriter, flusher http.Flusher, ev event.Event, kind event.EndpointKind, dir string) bool {
	payload, err := event.Wrap(ev, kind, dir)
	if err != nil {
		s.logger.Error("event wrap failed", "err", err)
		return false
	}
	if err := event.WriteFrame(w, flusher, payload); err != nil {
		return false
	}
	return true
}
