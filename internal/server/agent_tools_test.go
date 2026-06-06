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
