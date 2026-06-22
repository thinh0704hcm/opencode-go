package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/tool"
)

func TestExecuteTodoWriteToolAndTodoEndpoint(t *testing.T) {
	dir := t.TempDir()
	// create server with workdir
	srv := New(Options{Provider: provider.NewMock(""), Model: "mock", Workdir: dir})
	// create a session
	sess := srv.store.CreateSession("", "test", dir)

	sb, err := tool.New(dir)
	if err != nil {
		t.Fatalf("tool.New: %v", err)
	}

	call := provider.ToolCall{ID: "c1", Name: "todowrite", Input: json.RawMessage(`{"todos":[{"content":"c","status":"s","priority":"p"}]}`)}
	out, isErr := executeToolCall(withSessionID(context.Background(), sess.ID), srv.tools, sb, call)
	if isErr {
		t.Fatalf("todowrite expected success, got error: %s", out)
	}
	// output should be pretty JSON array
	if !strings.Contains(out, "\"content\": \"c\"") {
		t.Fatalf("unexpected output %q", out)
	}

	// Verify GET endpoint returns same todos
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/session/" + sess.ID + "/todo")
	if err != nil {
		t.Fatalf("GET todo: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET todo status = %d", resp.StatusCode)
	}
	var got []struct{ Content, Status, Priority string }
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if len(got) != 1 || got[0].Content != "c" || got[0].Status != "s" || got[0].Priority != "p" {
		t.Fatalf("unexpected todo list: %+v", got)
	}
}
