package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/tool"
)

func TestExecuteToolCallBashEcho(t *testing.T) {
	dir := t.TempDir()
	sb, err := tool.New(dir)
	if err != nil {
		t.Fatalf("tool.New: %v", err)
	}
	reg := tool.NewDefaultRegistry()
	call := provider.ToolCall{
		ID:    "call_1",
		Name:  "bash",
		Input: json.RawMessage(`{"command":"echo hi"}`),
	}
	out, isErr := executeToolCall(context.Background(), reg, sb, call)
	if isErr {
		t.Fatalf("expected isErr=false, got true; out=%q", out)
	}
	if got := strings.TrimSpace(out); got != "hi" {
		t.Fatalf("expected out=%q, got %q (raw=%q)", "hi", got, out)
	}
}

func TestExecuteToolCallRead(t *testing.T) {
	dir := t.TempDir()
	sb, err := tool.New(dir)
	if err != nil {
		t.Fatalf("tool.New: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello123"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	reg := tool.NewDefaultRegistry()
	call := provider.ToolCall{
		ID:    "c2",
		Name:  "read",
		Input: json.RawMessage(`{"path":"note.txt"}`),
	}
	out, isErr := executeToolCall(context.Background(), reg, sb, call)
	if isErr {
		t.Fatalf("expected isErr=false, got true; out=%q", out)
	}
	if !strings.Contains(out, "hello123") {
		t.Fatalf("expected out to contain %q, got %q", "hello123", out)
	}
}

func TestToolSchemasExposeDelegateAndTask(t *testing.T) {
	srv := New(Options{Provider: provider.NewMock("child ok"), Model: "mock", Workdir: t.TempDir()})
	schemas := toolSchemas(srv.tools, nil)
	seen := map[string]provider.ToolSchema{}
	for _, sc := range schemas {
		seen[sc.Name] = sc
	}
	for _, name := range []string{"delegate", "task"} {
		sc, ok := seen[name]
		if !ok {
			t.Fatalf("missing schema %q in %#v", name, seen)
		}
		if sc.Description == "" {
			t.Fatalf("schema %q has empty description", name)
		}
		props, ok := sc.Parameters["properties"].(map[string]any)
		if !ok {
			t.Fatalf("schema %q missing properties: %#v", name, sc.Parameters)
		}
		for _, field := range []string{"prompt", "description", "agent", "model"} {
			if _, ok := props[field]; !ok {
				t.Fatalf("schema %q missing field %q", name, field)
			}
		}
	}
}

func TestExecuteToolCallDelegateAndTaskReturnChildText(t *testing.T) {
	dir := t.TempDir()
	sb, err := tool.New(dir)
	if err != nil {
		t.Fatalf("tool.New: %v", err)
	}
	srv := New(Options{Provider: provider.NewMock("CHILD_OK"), Model: "mock", Workdir: dir})
	sess := srv.store.CreateSession("", "test", dir)

	cases := []provider.ToolCall{
		{ID: "d1", Name: "delegate", Input: json.RawMessage(`{"description":"return ok"}`)},
		{ID: "t1", Name: "task", Input: json.RawMessage(`{"prompt":"return ok"}`)},
	}
	ctx := withSessionID(context.Background(), sess.ID)
	for _, call := range cases {
		out, isErr := executeToolCall(ctx, srv.tools, sb, call)
		if isErr {
			t.Fatalf("%s expected success, got error output=%q", call.Name, out)
		}
		if !strings.Contains(out, "Delegated task") {
			t.Fatalf("%s expected delegate task output, got %q", call.Name, out)
		}
	}
}

func TestExecuteToolCallDelegateAndTaskRejectEmptyInput(t *testing.T) {
	dir := t.TempDir()
	sb, err := tool.New(dir)
	if err != nil {
		t.Fatalf("tool.New: %v", err)
	}
	srv := New(Options{Provider: provider.NewMock("unused"), Model: "mock", Workdir: dir})

	for _, name := range []string{"delegate", "task"} {
		out, isErr := executeToolCall(context.Background(), srv.tools, sb, provider.ToolCall{
			ID:    name + "_empty",
			Name:  name,
			Input: json.RawMessage(`{}`),
		})
		if !isErr {
			t.Fatalf("%s expected validation error, got success output=%q", name, out)
		}
		if !strings.Contains(out, "requires prompt or description") {
			t.Fatalf("%s expected validation message, got %q", name, out)
		}
	}
}
