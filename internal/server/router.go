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
	mux.HandleFunc("POST /session/{id}/todo", s.handleSessionTodoUpdate)
	mux.HandleFunc("PATCH /session/{id}/todo", s.handleSessionTodoUpdate)
	mux.HandleFunc("GET /session/{id}/diff", s.handleSessionDiff)
	mux.HandleFunc("POST /session/{id}/init", s.handleSessionNoop)
	mux.HandleFunc("POST /session/{id}/fork", s.handleSessionFork)
	mux.HandleFunc("POST /session/{id}/abort", s.handleSessionAbort)
	mux.HandleFunc("POST /session/{id}/share", s.handleSessionShare)
	mux.HandleFunc("DELETE /session/{id}/share", s.handleSessionUnshare)
	mux.HandleFunc("POST /session/{id}/summarize", s.handleSessionSummarize)

	// Prompt (async) + messages.
	mux.HandleFunc("POST /session/{id}/prompt_async", s.handlePromptAsync)
	mux.HandleFunc("POST /session/{id}/message", s.handlePrompt)
	mux.HandleFunc("POST /session/{id}/command", s.handleSessionCommand)
	mux.HandleFunc("POST /session/{id}/shell", s.handleSessionShell)
	mux.HandleFunc("POST /session/{id}/revert", s.handleSessionRevert)
	mux.HandleFunc("POST /session/{id}/unrevert", s.handleSessionUnrevert)
	mux.HandleFunc("GET /session/{id}/message", s.handleGetMessages)
	mux.HandleFunc("GET /session/{id}/message/{messageID}", s.handleGetMessage)

	// Read-only file search/read (real-server parity).
	mux.HandleFunc("GET /find/file", s.handleFindFile)
	mux.HandleFunc("GET /find", s.handleFind)
	mux.HandleFunc("GET /find/symbol", s.handleFindSymbol)
	mux.HandleFunc("GET /file", s.handleFileRead)
	mux.HandleFunc("GET /file/content", s.handleFileRead)
	mux.HandleFunc("GET /file/status", s.handleFileStatus)

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
	mux.HandleFunc("GET /project", s.handleProjectList)
	mux.HandleFunc("GET /project/current", s.handleProjectCurrent)
	mux.HandleFunc("GET /project/{id}/directories", s.handleProjectDirectories)
	mux.HandleFunc("GET /api/reference", s.handleAPIReference)
	mux.HandleFunc("GET /api/integration", s.handleAPIIntegration)
	mux.HandleFunc("POST /provider/{id}/oauth/authorize", s.handleProviderOAuthNoop)
	mux.HandleFunc("POST /provider/{id}/oauth/callback", s.handleProviderOAuthNoop)
	mux.HandleFunc("GET /provider/auth", s.handleProviderAuth)
	mux.HandleFunc("GET /experimental/console", s.handleExperimentalConsole)
	mux.HandleFunc("GET /experimental/tool/ids", s.handleExperimentalToolIDs)
	mux.HandleFunc("GET /experimental/tool", s.handleExperimentalTool)

	// M2 sub2 boot stubs: empty/exact-shape stubs.
	mux.HandleFunc("GET /command", s.handleCommand)
	mux.HandleFunc("GET /skill", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	})
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
	mux.HandleFunc("POST /experimental/session/{id}/background", s.handleExperimentalSessionBackground)

	// TUI control long-poll + log sink.
	mux.HandleFunc("GET /tui/control/next", s.handleTUIControlNext)
	mux.HandleFunc("POST /tui/control/response", s.handleTUIOK)
	mux.HandleFunc("POST /tui/append-prompt", s.handleTUIOK)
	mux.HandleFunc("POST /tui/open-help", s.handleTUIOK)
	mux.HandleFunc("POST /tui/open-sessions", s.handleTUIOK)
	mux.HandleFunc("POST /tui/open-themes", s.handleTUIOK)
	mux.HandleFunc("POST /tui/open-models", s.handleTUIOK)
	mux.HandleFunc("POST /tui/submit-prompt", s.handleTUIOK)
	mux.HandleFunc("POST /tui/clear-prompt", s.handleTUIOK)
	mux.HandleFunc("POST /tui/execute-command", s.handleTUIOK)
	mux.HandleFunc("POST /tui/show-toast", s.handleTUIOK)
	mux.HandleFunc("POST /tui/publish", s.handleTUIPublish)
	mux.HandleFunc("POST /instance/dispose", s.handleTUIOK)
	mux.HandleFunc("POST /log", s.handleLog)

	// SDK Drop-in: Missing v1 routes
	mux.HandleFunc("PATCH /config", s.handleConfigUpdate)
	mux.HandleFunc("POST /mcp", s.handleMCPAdd)
	mux.HandleFunc("PUT /auth/{id}", s.handleAuthSet)
	mux.HandleFunc("DELETE /mcp/{name}/auth", s.handleMCPAuthRemove)

	// SDK Drop-in: Missing v2 global routes
	mux.HandleFunc("GET /global/config", s.handleGlobalConfigGet)
	mux.HandleFunc("PATCH /global/config", s.handleGlobalConfigUpdate)
	mux.HandleFunc("POST /global/dispose", s.handleTUIOK)
	mux.HandleFunc("POST /global/upgrade", s.handleTUIOK)
	mux.HandleFunc("DELETE /auth/{providerID}", s.handleAuthRemove)
	// PUT /auth/{providerID} is covered by PUT /auth/{id} above

	// SDK Drop-in: Missing experimental stubs
	mux.HandleFunc("GET /experimental/console/orgs", s.handleExperimentalConsoleOrgs)
	mux.HandleFunc("POST /experimental/console/switch", s.handleTUIOK)
	mux.HandleFunc("GET /experimental/session", s.handleExperimentalSessionList)
	mux.HandleFunc("POST /experimental/control-plane/move-session", s.handleTUIOK)
	mux.HandleFunc("GET /experimental/workspace/adapter", s.handleExperimentalWorkspaceAdapter)
	mux.HandleFunc("POST /experimental/workspace", s.handleTUIOK)
	mux.HandleFunc("POST /experimental/workspace/sync-list", s.handleTUIOK)
	mux.HandleFunc("DELETE /experimental/workspace/{id}", s.handleTUIOK)
	mux.HandleFunc("POST /experimental/workspace/warp", s.handleTUIOK)
	mux.HandleFunc("GET /experimental/worktree", s.handleExperimentalWorktreeList)
	mux.HandleFunc("POST /experimental/worktree", s.handleTUIOK)
	mux.HandleFunc("DELETE /experimental/worktree", s.handleTUIOK)
	mux.HandleFunc("POST /experimental/worktree/reset", s.handleTUIOK)
	mux.HandleFunc("POST /experimental/project/{projectID}/copy", s.handleTUIOK)
	mux.HandleFunc("DELETE /experimental/project/{projectID}/copy", s.handleTUIOK)
	mux.HandleFunc("POST /experimental/project/{projectID}/copy/refresh", s.handleTUIOK)

	// v2 API
	mux.HandleFunc("GET /api/health", s.handleV2Health)
	mux.HandleFunc("GET /api/location", s.handleV2Location)
	mux.HandleFunc("GET /api/agent", s.handleV2AgentList)
	mux.HandleFunc("GET /api/model", s.handleV2ModelList)
	mux.HandleFunc("GET /api/provider", s.handleV2ProviderList)
	mux.HandleFunc("GET /api/provider/{providerID}", s.handleV2ProviderGet)
	mux.HandleFunc("GET /api/session", s.handleV2SessionList)
	mux.HandleFunc("POST /api/session", s.handleV2SessionCreate)
	mux.HandleFunc("GET /api/session/{sessionID}", s.handleV2SessionGet)
	mux.HandleFunc("POST /api/session/{sessionID}/prompt", s.handleV2SessionPrompt)
	mux.HandleFunc("GET /api/session/{id}/todo", s.handleSessionTodo)
	mux.HandleFunc("POST /api/session/{id}/todo", s.handleSessionTodoUpdate)
	mux.HandleFunc("PATCH /api/session/{id}/todo", s.handleSessionTodoUpdate)
	mux.HandleFunc("GET /api/session/{sessionID}/wait", s.handleV2SessionWait)
	mux.HandleFunc("GET /api/session/{sessionID}/message", s.handleV2SessionMessages)
	mux.HandleFunc("GET /api/session/{sessionID}/event", s.handleV2SessionEvent)
	// v2 global event stream (used by newer TUI clients via /api/event?location[directory]=...)
	mux.HandleFunc("GET /api/event", s.handleV2GlobalEvent)
	// v2 stubs: permission, command, skill, filesystem, session lifecycle
	mux.HandleFunc("GET /api/command", s.handleV2CommandList)
	mux.HandleFunc("GET /api/skill", s.handleV2SkillList)
	mux.HandleFunc("GET /api/permission/request", s.handleV2PermissionRequestList)
	mux.HandleFunc("GET /api/permission/saved", s.handleV2PermissionSavedList)
	mux.HandleFunc("DELETE /api/permission/saved/{id}", s.handleV2PermissionSavedDelete)
	mux.HandleFunc("GET /api/session/{sessionID}/permission/request", s.handleV2SessionPermissionRequestList)
	mux.HandleFunc("GET /api/session/{sessionID}/context", s.handleV2SessionContext)
	mux.HandleFunc("POST /api/session/{sessionID}/compact", s.handleV2SessionCompact)
	mux.HandleFunc("POST /api/session/{sessionID}/permission/request/{requestID}/reply", s.handleV2SessionPermissionReply)
	mux.HandleFunc("POST /api/session/{sessionID}/question/request/{requestID}/reply", s.handleV2SessionQuestionReply)
	mux.HandleFunc("POST /api/session/{sessionID}/question/request/{requestID}/reject", s.handleV2SessionQuestionReject)
	mux.HandleFunc("GET /api/question/request", s.handleV2QuestionRequestList)
	mux.HandleFunc("POST /tui/select-session", s.handleTUIOK)
	mux.HandleFunc("GET /api/fs/list", s.handleV2FSList)
	mux.HandleFunc("GET /api/fs/read", s.handleV2FSRead)

	return s.loggingMiddleware(mux)
}

// directoryOf extracts the optional ?directory=<cwd> query param.
func directoryOf(r *http.Request) string {
	if dir := r.URL.Query().Get("directory"); dir != "" {
		return dir
	}
	return r.URL.Query().Get("path")
}
