package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func runTool(t *testing.T, r *Registry, name string, sb *Sandbox, in any) (Result, error) {
	t.Helper()
	tl, ok := r.Lookup(name)
	if !ok {
		t.Fatalf("tool %q not found in registry", name)
	}
	return tl.Execute(context.Background(), raw(t, in), sb)
}

func TestSandboxRejectsTraversal(t *testing.T) {
	tmp := t.TempDir()
	sb, err := New(tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := sb.Resolve("../escape"); err == nil {
		t.Errorf("Resolve(\"../escape\") = nil error, want error")
	}
	if _, err := sb.Resolve("/etc/passwd"); err == nil {
		t.Errorf("Resolve(\"/etc/passwd\") = nil error, want error")
	}
	if _, err := sb.Resolve("sub/../ok.txt"); err != nil {
		t.Errorf("Resolve(\"sub/../ok.txt\") = %v, want nil error", err)
	}
}

func TestSandboxAllowsNewFileCreate(t *testing.T) {
	tmp := t.TempDir()
	sb, err := New(tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := sb.Resolve("newfile.txt"); err != nil {
		t.Errorf("Resolve(\"newfile.txt\") = %v, want nil error", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "newdir"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if _, err := sb.Resolve("newdir/inner.txt"); err != nil {
		t.Errorf("Resolve(\"newdir/inner.txt\") = %v, want nil error", err)
	}
}

func TestSandboxSymlinkEscape(t *testing.T) {
	tmp := t.TempDir()
	sb, err := New(tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := os.Symlink("/etc", filepath.Join(tmp, "link")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if _, err := sb.Resolve("link/passwd"); err == nil {
		t.Errorf("Resolve(\"link/passwd\") = nil error, want error (symlink escape)")
	}
}

func TestWriteThenRead(t *testing.T) {
	tmp := t.TempDir()
	sb, err := New(tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r := NewDefaultRegistry()

	if _, err := runTool(t, r, "write", sb, map[string]string{"path": "a.txt", "content": "hello"}); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	res, err := runTool(t, r, "read", sb, map[string]string{"path": "a.txt"})
	if err != nil {
		t.Fatalf("read a.txt: %v", err)
	}
	if res.Output != "hello" {
		t.Errorf("read output = %q, want %q", res.Output, "hello")
	}

	if _, err := runTool(t, r, "write", sb, map[string]string{"path": "../bad", "content": "x"}); err == nil {
		t.Errorf("write ../bad = nil error, want error")
	}
}

func TestEditReplacesFirst(t *testing.T) {
	tmp := t.TempDir()
	sb, err := New(tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r := NewDefaultRegistry()

	if _, err := runTool(t, r, "write", sb, map[string]string{"path": "e.txt", "content": "foo foo"}); err != nil {
		t.Fatalf("write e.txt: %v", err)
	}
	if _, err := runTool(t, r, "edit", sb, map[string]string{"path": "e.txt", "old": "foo", "new": "bar"}); err != nil {
		t.Fatalf("edit e.txt: %v", err)
	}
	res, err := runTool(t, r, "read", sb, map[string]string{"path": "e.txt"})
	if err != nil {
		t.Fatalf("read e.txt: %v", err)
	}
	if res.Output != "bar foo" {
		t.Errorf("after edit = %q, want %q", res.Output, "bar foo")
	}

	if _, err := runTool(t, r, "edit", sb, map[string]string{"path": "e.txt", "old": "missing", "new": "x"}); err == nil {
		t.Errorf("edit with absent old = nil error, want error")
	}
}

func TestBashEcho(t *testing.T) {
	tmp := t.TempDir()
	sb, err := New(tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r := NewDefaultRegistry()

	res, err := runTool(t, r, "bash", sb, map[string]string{"command": "echo hi"})
	if err != nil {
		t.Fatalf("bash echo hi: %v", err)
	}
	if !strings.Contains(res.Output, "hi") {
		t.Errorf("bash output = %q, want substring %q", res.Output, "hi")
	}

	res, err = runTool(t, r, "bash", sb, map[string]string{"command": "echo $PWD"})
	if err != nil {
		t.Fatalf("bash echo PWD: %v", err)
	}
	base := filepath.Base(sb.Root())
	if !strings.Contains(res.Output, base) {
		t.Errorf("bash PWD = %q, want substring %q (sandbox root basename)", res.Output, base)
	}
}

func TestLsGlobGrep(t *testing.T) {
	tmp := t.TempDir()
	sb, err := New(tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r := NewDefaultRegistry()

	if _, err := runTool(t, r, "write", sb, map[string]string{"path": "a.txt", "content": "hello"}); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if _, err := runTool(t, r, "write", sb, map[string]string{"path": "b.txt", "content": "world"}); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	lsRes, err := runTool(t, r, "ls", sb, map[string]string{"path": "."})
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if !strings.Contains(lsRes.Output, "a.txt") || !strings.Contains(lsRes.Output, "b.txt") {
		t.Errorf("ls output = %q, want both a.txt and b.txt", lsRes.Output)
	}

	globRes, err := runTool(t, r, "glob", sb, map[string]string{"pattern": "*.txt"})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if !strings.Contains(globRes.Output, "a.txt") || !strings.Contains(globRes.Output, "b.txt") {
		t.Errorf("glob output = %q, want both a.txt and b.txt", globRes.Output)
	}

	grepRes, err := runTool(t, r, "grep", sb, map[string]string{"pattern": "hello", "path": "."})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(grepRes.Output, "hello") {
		t.Errorf("grep output = %q, want match line containing %q", grepRes.Output, "hello")
	}
}

func TestTruncateOutput(t *testing.T) {
	big := make([]byte, MaxOutputBytes+1024)
	for i := range big {
		big[i] = 'a'
	}
	out, truncated := TruncateOutput(big)
	if !truncated {
		t.Errorf("TruncateOutput truncated = false, want true for input > MaxOutputBytes")
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("TruncateOutput output does not contain %q", "truncated")
	}

	small := []byte("short")
	out, truncated = TruncateOutput(small)
	if truncated {
		t.Errorf("TruncateOutput truncated = true, want false for small input")
	}
	if out != "short" {
		t.Errorf("TruncateOutput output = %q, want %q", out, "short")
	}
}
