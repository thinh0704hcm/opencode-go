package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/session"
	"github.com/opencode-go/opencode-go/internal/tool"
)

// todoWriteTool implements the server‑owned "todowrite" tool.
// It mutates the session's todo list and publishes a todo.updated event.
type todoWriteTool struct{ srv *Server }

func (todoWriteTool) Name() string   { return "todowrite" }
func (todoWriteTool) Mutating() bool { return true }

type todoWriteInput struct {
	Todos []session.Todo `json:"todos"`
}

func (t todoWriteTool) Execute(ctx context.Context, input json.RawMessage, sb *tool.Sandbox) (tool.Result, error) {
	var in todoWriteInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tool.Result{}, fmt.Errorf("todowrite: invalid JSON: %w", err)
	}
	sessionID := sessionIDFromCtx(ctx)
	if sessionID == "" {
		return tool.Result{}, fmt.Errorf("todowrite: missing session ID in context")
	}
	t.srv.store.SetTodos(sessionID, in.Todos)
	t.srv.bus.Publish(event.NewTodoUpdated(sessionID, in.Todos))
	outBytes, err := json.MarshalIndent(in.Todos, "", "  ")
	if err != nil {
		return tool.Result{}, err
	}
	return tool.Result{Output: string(outBytes), Truncated: false}, nil
}
