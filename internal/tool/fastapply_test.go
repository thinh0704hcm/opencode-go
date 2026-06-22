package tool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFastApplyWriteAndEdit(t *testing.T) {
	tmp := t.TempDir()
	sb, err := New(tmp)
	if err != nil {
		t.Fatalf("sandbox New: %v", err)
	}
	r := NewDefaultRegistry()
	r.Register(NewFastApplyTool())

	// Write operation
	writeOps := []fastApplyOp{{Path: "a.txt", Mode: "write", Content: "hello"}}
	if _, err := runTool(t, r, "fastapply", sb, map[string]any{"operations": writeOps}); err != nil {
		t.Fatalf("fastapply write: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, "a.txt"))
	if err != nil {
		t.Fatalf("read a.txt: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("a.txt content = %q, want %q", string(data), "hello")
	}

	// Edit operation (unique old)
	if err := os.WriteFile(filepath.Join(tmp, "b.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("setup b.txt: %v", err)
	}
	editOps := []fastApplyOp{{Path: "b.txt", Mode: "edit", Content: "there", Old: "world"}}
	if _, err := runTool(t, r, "fastapply", sb, map[string]any{"operations": editOps}); err != nil {
		t.Fatalf("fastapply edit unique: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(tmp, "b.txt"))
	if err != nil {
		t.Fatalf("read b.txt: %v", err)
	}
	if string(data) != "hello there" {
		t.Fatalf("b.txt content = %q, want %q", string(data), "hello there")
	}

	// Edit operation (non‑unique old) should error and not modify file.
	if err := os.WriteFile(filepath.Join(tmp, "c.txt"), []byte("foo bar foo"), 0o644); err != nil {
		t.Fatalf("setup c.txt: %v", err)
	}
	badEdit := []fastApplyOp{{Path: "c.txt", Mode: "edit", Content: "baz", Old: "foo"}}
	if _, err := runTool(t, r, "fastapply", sb, map[string]any{"operations": badEdit}); err == nil {
		t.Fatalf("expected error for non‑unique old string")
	}
	data, err = os.ReadFile(filepath.Join(tmp, "c.txt"))
	if err != nil {
		t.Fatalf("read c.txt: %v", err)
	}
	if string(data) != "foo bar foo" {
		t.Fatalf("c.txt should be unchanged, got %q", string(data))
	}

	// Out‑of‑root path should error.
	badPath := []fastApplyOp{{Path: "../bad.txt", Mode: "write", Content: "x"}}
	if _, err := runTool(t, r, "fastapply", sb, map[string]any{"operations": badPath}); err == nil {
		t.Fatalf("expected error for path escaping sandbox")
	}
}
