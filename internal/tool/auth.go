package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type authStatusTool struct{}

type authHintTool struct{}

func (authStatusTool) Name() string   { return "auth_status" }
func (authStatusTool) Mutating() bool { return false }

func (authHintTool) Name() string   { return "auth_hint" }
func (authHintTool) Mutating() bool { return false }

func loadAuthMap(sb *Sandbox) (map[string]bool, error) {
	// Search project then home auth files, validate permissions and bool values only.
	candidates := []string{}
	if sb != nil {
		candidates = append(candidates, filepath.Join(sb.Root(), "auth.json"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "auth.json"))
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		// Unix permission check: reject if group/other readable.
		if fi, err2 := os.Stat(p); err2 == nil {
			if mode := fi.Mode().Perm(); mode&0o077 != 0 {
				// insecure file, skip
				continue
			}
		}
		// Unmarshal into generic map to allow only bool values.
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		m := make(map[string]bool)
		for k, v := range raw {
			if b, ok := v.(bool); ok {
				m[k] = b
			}
		}
		return m, nil
	}
	// No auth file found; return empty map without error.
	return make(map[string]bool), nil
}

func (authStatusTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	// Validate provider against allowed list
	allowed := map[string]bool{"openai-codex": true, "gemini": true, "google-antigravity": true, "cursor": true}
	if !allowed[in.Provider] {
		return Result{}, fmt.Errorf("unsupported provider: %s", in.Provider)
	}
	// Load auth.json map (may be empty)
	m, err := loadAuthMap(sb)
	if err != nil {
		return Result{}, err
	}
	authenticated := m[in.Provider]
	// Env var checks – provider‑specific markers indicate auth without exposing values
	envChecks := map[string][]string{
		"openai-codex":       {"OPENCODE_AUTH_OPENAI_CODEX", "OPENAI_API_KEY"},
		"gemini":             {"OPENCODE_AUTH_GEMINI", "GEMINI_API_KEY", "GOOGLE_API_KEY"},
		"google-antigravity": {"OPENCODE_AUTH_GOOGLE_ANTIGRAVITY", "GOOGLE_ANTIGRAVITY_TOKEN"},
		"cursor":             {"OPENCODE_AUTH_CURSOR", "CURSOR_API_KEY"},
	}
	if vars, ok := envChecks[in.Provider]; ok {
		for _, v := range vars {
			if val, present := os.LookupEnv(v); present && strings.TrimSpace(val) != "" {
				authenticated = true
				break
			}
		}
	}
	// Return JSON without exposing any secret values
	outBytes, err := json.Marshal(map[string]any{"provider": in.Provider, "authenticated": authenticated})
	if err != nil {
		return Result{}, err
	}
	return Result{Output: string(outBytes)}, nil
}

func (authHintTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	// Validate provider
	allowed := map[string]bool{"openai-codex": true, "gemini": true, "google-antigravity": true, "cursor": true}
	if !allowed[in.Provider] {
		return Result{}, fmt.Errorf("unsupported provider: %s", in.Provider)
	}
	// Convert provider name to env‑friendly format (hyphens → underscores)
	envName := strings.ReplaceAll(strings.ToUpper(in.Provider), "-", "_")
	hint := fmt.Sprintf("Set environment variable OPENCODE_AUTH_%s=1 or provide API key in auth.json", envName)
	outBytes, err := json.Marshal(map[string]any{"provider": in.Provider, "hint": hint})
	if err != nil {
		return Result{}, err
	}
	return Result{Output: string(outBytes)}, nil
}
