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

	// Session list (method-distinct from POST /session above).
	mux.HandleFunc("GET /session", s.handleSessionList)

	// Session lifecycle (get/update/delete/children/abort).
	mux.HandleFunc("GET /session/{id}", s.handleSessionGet)
	mux.HandleFunc("PATCH /session/{id}", s.handleSessionUpdate)
	mux.HandleFunc("DELETE /session/{id}", s.handleSessionDelete)
	mux.HandleFunc("GET /session/{id}/children", s.handleSessionChildren)
	mux.HandleFunc("GET /session/{id}/todo", s.handleSessionTodo)
	mux.HandleFunc("GET /session/{id}/diff", s.handleSessionDiff)
	mux.HandleFunc("POST /session/{id}/abort", s.handleSessionAbort)

	// Prompt (async) + messages.
	mux.HandleFunc("POST /session/{id}/prompt_async", s.handlePromptAsync)
	mux.HandleFunc("POST /session/{id}/message", s.handlePrompt)
	mux.HandleFunc("POST /session/{id}/shell", s.handleSessionShell)
	mux.HandleFunc("GET /session/{id}/message", s.handleGetMessages)
	mux.HandleFunc("GET /session/{id}/message/{messageID}", s.handleGetMessage)

	// Read-only file search/read (real-server parity).
	mux.HandleFunc("GET /find/file", s.handleFindFile)
	mux.HandleFunc("GET /file", s.handleFileRead)

	// Permission reply: primary + fallback, both wired to one gate (§4.2/B2).
	mux.HandleFunc("POST /permission/{requestID}/reply", s.handlePermissionReply)
	mux.HandleFunc("POST /session/{sessionID}/permissions/{permissionID}", s.handlePermissionRespond)

	// M2 boot/config + provider registry (real data; apiKey masked, §3.4/§3.5).
	mux.HandleFunc("GET /config", s.handleConfigGet)
	mux.HandleFunc("GET /config/providers", s.handleConfigProviders)
	mux.HandleFunc("GET /provider", s.handleProvider)
	mux.HandleFunc("GET /agent", s.handleAgent)

	// M2 sub2 boot stubs: populated stubs.
	mux.HandleFunc("GET /path", s.handlePath)
	mux.HandleFunc("GET /project/current", s.handleProjectCurrent)
	mux.HandleFunc("GET /provider/auth", s.handleProviderAuth)
	mux.HandleFunc("GET /experimental/console", s.handleExperimentalConsole)

	// M2 sub2 boot stubs: empty/exact-shape stubs.
	mux.HandleFunc("GET /command", s.handleCommand)
	mux.HandleFunc("GET /mcp", s.handleMCP)
	mux.HandleFunc("POST /mcp/{name}/connect", s.handleMCPConnect)
	mux.HandleFunc("POST /mcp/{name}/disconnect", s.handleMCPDisconnect)
	mux.HandleFunc("POST /mcp/{name}/auth", s.handleMCPAuth)
	mux.HandleFunc("POST /mcp/{name}/auth/authenticate", s.handleMCPAuthAuthenticate)
	mux.HandleFunc("POST /mcp/{name}/auth/callback", s.handleMCPAuthCallback)

	// PTY namespace routes.
	mux.HandleFunc("GET /pty", s.handlePtyList)
	mux.HandleFunc("POST /pty", s.handlePtyCreate)
	mux.HandleFunc("GET /pty/shells", s.handlePtyShells)
	mux.HandleFunc("GET /pty/{ptyID}", s.handlePtyGet)
	mux.HandleFunc("PUT /pty/{ptyID}", s.handlePtyUpdate)
	mux.HandleFunc("DELETE /pty/{ptyID}", s.handlePtyRemove)
	mux.HandleFunc("POST /pty/{ptyID}/connect-token", s.handlePtyConnectToken)
	mux.HandleFunc("GET /pty/{ptyID}/connect", s.handlePtyConnect)

	mux.HandleFunc("GET /formatter", s.handleFormatter)
	mux.HandleFunc("GET /lsp", s.handleLSP)
	mux.HandleFunc("GET /session/status", s.handleSessionStatus)
	mux.HandleFunc("GET /vcs", s.handleVCS)
	mux.HandleFunc("GET /vcs/status", s.handleVCSStatus)
	mux.HandleFunc("GET /vcs/diff", s.handleVCSDiff)
	mux.HandleFunc("GET /vcs/diff/raw", s.handleVCSDiffRaw)
	mux.HandleFunc("POST /vcs/apply", s.handleVCSApply)
	mux.HandleFunc("GET /experimental/resource", s.handleExperimentalResource)
	mux.HandleFunc("GET /experimental/workspace", s.handleExperimentalWorkspace)
	mux.HandleFunc("GET /experimental/workspace/status", s.handleExperimentalWorkspaceStatus)

	// TUI control long-poll + log sink.
	mux.HandleFunc("GET /tui/control/next", s.handleTUIControlNext)
	mux.HandleFunc("POST /log", s.handleLog)

	return s.loggingMiddleware(mux)
}

// directoryOf extracts the optional ?directory=<cwd> query param.
func directoryOf(r *http.Request) string {
	return r.URL.Query().Get("directory")
}
