package server

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/opencode-go/opencode-go/internal/tool"
)

// todoReadTool implements the "todoread" tool – read-only view of session todos.
type todoReadTool struct{ srv *Server }

func (todoReadTool) Name() string   { return "todoread" }
func (todoReadTool) Mutating() bool { return false }

func (t todoReadTool) Execute(ctx context.Context, input json.RawMessage, sb *tool.Sandbox) (tool.Result, error) {
    // Input ignored – schema has empty object.
    sessionID := sessionIDFromCtx(ctx)
    if sessionID == "" {
        return tool.Result{}, fmt.Errorf("todoread: missing session ID in context")
    }
    todos, _ := t.srv.store.GetTodos(sessionID)
    // Ensure JSON output (pretty printed) even if empty slice.
    outBytes, err := json.MarshalIndent(todos, "", "  ")
    if err != nil {
        return tool.Result{}, err
    }
    return tool.Result{Output: string(outBytes), Truncated: false}, nil
}
