package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthTools(t *testing.T) {
	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox new: %v", err)
	}
	// create auth.json in project root
	authPath := filepath.Join(sb.Root(), "auth.json")
	data := []byte(`{"openai-codex": true, "gemini": false}`)
	if err := os.WriteFile(authPath, data, 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	r := NewDefaultRegistry()
	// status true from file
	res, err := runTool(t, r, "auth_status", sb, map[string]string{"provider": "openai-codex"})
	if err != nil {
		t.Fatalf("auth_status: %v", err)
	}
	var out struct {
		Provider      string `json:"provider"`
		Authenticated bool   `json:"authenticated"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Authenticated {
		t.Errorf("expected authenticated true from auth.json")
	}
	// hint uses underscore
	hintRes, err := runTool(t, r, "auth_hint", sb, map[string]string{"provider": "gemini"})
	if err != nil {
		t.Fatalf("auth_hint: %v", err)
	}
	var hintOut struct {
		Provider string `json:"provider"`
		Hint     string `json:"hint"`
	}
	if err := json.Unmarshal([]byte(hintRes.Output), &hintOut); err != nil {
		t.Fatalf("unmarshal hint: %v", err)
	}
	if !strings.Contains(hintOut.Hint, "OPENCODE_AUTH_GEMINI") {
		t.Errorf("hint missing underscore var: %s", hintOut.Hint)
	}
	// env auth without auth.json
	// new sandbox without auth file
	sb2, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox new2: %v", err)
	}
	t.Setenv("OPENCODE_AUTH_OPENAI_CODEX", "1")
	res2, err := runTool(t, r, "auth_status", sb2, map[string]string{"provider": "openai-codex"})
	if err != nil {
		t.Fatalf("auth_status env: %v", err)
	}
	var out2 struct {
		Provider      string `json:"provider"`
		Authenticated bool   `json:"authenticated"`
	}
	if err := json.Unmarshal([]byte(res2.Output), &out2); err != nil {
		t.Fatalf("unmarshal env: %v", err)
	}
	if !out2.Authenticated {
		t.Errorf("expected authenticated true from env")
	}
}
