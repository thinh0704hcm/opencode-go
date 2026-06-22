//go:build opencode_wip

package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/session"
	"github.com/opencode-go/opencode-go/internal/tool"
)

// goalsWriteTool implements the server‑owned "goalswrite" tool.
// It mutates the session's goal list and publishes a todo.updated event (re‑used).
type goalWriteTool struct{ srv *Server }

func (goalWriteTool) Name() string   { return "goalswrite" }
func (goalWriteTool) Mutating() bool { return true }

type goalWriteInput struct {
	Goals []session.Goal `json:"goals"`
}

func (t goalWriteTool) Execute(ctx context.Context, input json.RawMessage, sb *tool.Sandbox) (tool.Result, error) {
	var in goalWriteInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tool.Result{}, fmt.Errorf("goalswrite: invalid JSON: %w", err)
	}
	sessionID := sessionIDFromCtx(ctx)
	if sessionID == "" {
		return tool.Result{}, fmt.Errorf("goalswrite: missing session ID in context")
	}
	t.srv.store.SetGoals(sessionID, in.Goals)
	// Re‑use todo.updated event for simplicity.
	t.srv.bus.Publish(event.NewTodoUpdated(sessionID, in.Goals))
	outBytes, err := json.MarshalIndent(in.Goals, "", "  ")
	if err != nil {
		return tool.Result{}, err
	}
	return tool.Result{Output: string(outBytes), Truncated: false}, nil
}
