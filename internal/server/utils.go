package server

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/opencode-go/opencode-go/internal/session"
)



// parentAssistantMutable returns true only if the assistant message is eligible
// for task result injection (restart resumption).
func parentAssistantMutable(m *session.MessageWithParts) bool {
	if m == nil || m.Info.Role != "assistant" {
		return false
	}
	// completed tool_calls parent delegate turns are intentionally mutable for async task result injection.
	if m.Info.Finish == "tool_calls" {
		return true
	}
	// Completed turn is not mutable
	if m.Info.Time.Completed != nil {
		return false
	}
	// Active turn that is not a terminal final answer
	if m.Info.Finish == "error" || m.Info.Finish == "aborted" {
		return false
	}
	return true
}
func (s *Server) resolveWorkdirForRequest(r *http.Request) string {
	if d := directoryOf(r); d != "" {
		return filepath.Clean(d)
	}
	if s.workdir != "" && s.workdir != "." {
		return s.workdir
	}
	if d := os.Getenv("OPENCODE_GO_WORKDIR"); d != "" {
		return d
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return s.workdir
}
