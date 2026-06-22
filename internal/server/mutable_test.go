//go:build opencode_recovery_wip

package server

import (
	"testing"

	"github.com/opencode-go/opencode-go/internal/session"
)

func TestParentAssistantMutable(t *testing.T) {
	// active, Finish="stop", Completed nil -> true
	m1 := &session.MessageWithParts{Info: session.Message{Role: "assistant", Finish: "stop"}}
	if !parentAssistantMutable(m1) {
		t.Error("active parent with stop should be mutable")
	}

	// completed, Finish="stop" -> false
	now := int64(123)
	m2 := &session.MessageWithParts{Info: session.Message{Role: "assistant", Finish: "stop", Time: session.Time{Completed: &now}}}
	if parentAssistantMutable(m2) {
		t.Error("completed parent with stop should not be mutable")
	}

	// completed, Finish="tool_calls" -> true
	m3 := &session.MessageWithParts{Info: session.Message{Role: "assistant", Finish: "tool_calls", Time: session.Time{Completed: &now}}}
	if !parentAssistantMutable(m3) {
		t.Error("completed parent with tool_calls should be mutable")
	}
}
