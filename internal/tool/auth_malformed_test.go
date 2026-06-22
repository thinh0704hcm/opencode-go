package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthMalformedJSON(t *testing.T) {
	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox new: %v", err)
	}
	// write malformed auth.json
	authPath := filepath.Join(sb.Root(), "auth.json")
	if err := os.WriteFile(authPath, []byte("{invalid json"), 0o600); err != nil {
		t.Fatalf("write malformed auth: %v", err)
	}
	r := NewDefaultRegistry()
	res, err := runTool(t, r, "auth_status", sb, map[string]string{"provider": "openai-codex"})
	if err != nil {
		t.Fatalf("auth_status error: %v", err)
	}
	var out struct {
		Provider      string `json:"provider"`
		Authenticated bool   `json:"authenticated"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Should be false because malformed file is ignored
	if out.Authenticated {
		t.Errorf("expected authenticated false with malformed auth.json")
	}
}

func TestMemoryAddNoWritePermission(t *testing.T) {
	// sandbox root with read-only perms
	dir := t.TempDir()
	// remove write permission
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	sb, err := New(dir)
	if err != nil {
		t.Fatalf("sandbox new: %v", err)
	}
	r := NewDefaultRegistry()
	_, err = runTool(t, r, "memory", sb, map[string]string{"action": "add", "scope": "project", "key": "k", "value": "v"})
	if err == nil {
		t.Fatalf("expected error due to write permission denied")
	}
	// restore perms for cleanup (optional)
	_ = os.Chmod(dir, 0o700)
}
