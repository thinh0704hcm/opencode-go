package server

import (
	"net/http"
)

// routes builds the stdlib ServeMux with Go 1.22 method+path patterns.
// The optional ?directory=<cwd> query param is threaded through handlers but
// may be ignored in M1 (architecture §0b/§1).
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Health: /global/health and the bot-probed alias /api/global/health.
	mux.HandleFunc("GET /global/health", s.handleHealth)
	mux.HandleFunc("GET /api/global/health", s.handleHealth)

	// SSE event streams.
	mux.HandleFunc("GET /global/event", s.handleGlobalEvent)
	mux.HandleFunc("GET /event", s.handleBareEvent)

	// Session create.
	mux.HandleFunc("POST /session", s.handleSessionCreate)

	// Prompt (async) + messages.
	mux.HandleFunc("POST /session/{id}/prompt_async", s.handlePromptAsync)
	mux.HandleFunc("GET /session/{id}/message", s.handleGetMessages)

	// Permission reply: primary + fallback, both wired to one gate (§4.2/B2).
	mux.HandleFunc("POST /permission/{requestID}/reply", s.handlePermissionReply)
	mux.HandleFunc("POST /session/{sessionID}/permissions/{permissionID}", s.handlePermissionRespond)

	return mux
}

// directoryOf extracts the optional ?directory=<cwd> query param.
func directoryOf(r *http.Request) string {
	return r.URL.Query().Get("directory")
}
