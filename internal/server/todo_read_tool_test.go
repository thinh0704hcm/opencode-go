package server

import (
    "context"
    "encoding/json"
    "testing"
    "strings"

    "github.com/opencode-go/opencode-go/internal/provider"
    "github.com/opencode-go/opencode-go/internal/tool"
    "github.com/opencode-go/opencode-go/internal/session"
)

func TestExecuteTodoReadTool(t *testing.T) {
    srv := New(Options{Provider: provider.NewMock(""), Model: "mock", Workdir: t.TempDir()})
    // create session and write some todos
    sess := srv.store.CreateSession("", "test", srv.workdir)
    srv.store.SetTodos(sess.ID, []session.Todo{{Content: "c", Status: "s", Priority: "p"}})
    sb, err := tool.New(srv.workdir)
    if err != nil {
        t.Fatalf("tool.New: %v", err)
    }
    // call todoread tool
    call := provider.ToolCall{ID: "c1", Name: "todoread", Input: json.RawMessage(`{}`)}
    out, isErr := executeToolCall(withSessionID(context.Background(), sess.ID), srv.tools, sb, call)
    if isErr {
        t.Fatalf("todoread expected success, got error: %s", out)
    }
    if !strings.Contains(out, "\"content\": \"c\"") {
        t.Fatalf("unexpected output %q", out)
    }
}
