package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAgentsParsesFrontmatterAndBody(t *testing.T) {
	dir := t.TempDir()
	adir := filepath.Join(dir, ".opencode", "agent")
	if err := os.MkdirAll(adir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: reviewer\ndescription: Reviews code.\nmodel: cx/gpt-5.5-review\ntemperature: 0.2\n---\nYou are a meticulous code reviewer.\nBe concise.\n"
	if err := os.WriteFile(filepath.Join(adir, "reviewer.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	a, ok := resolveAgent(dir, "reviewer")
	if !ok {
		t.Fatal("agent not resolved")
	}
	if a.Description != "Reviews code." || a.Model != "cx/gpt-5.5-review" {
		t.Fatalf("frontmatter lost: %+v", a)
	}
	if a.Temperature == nil || *a.Temperature != 0.2 {
		t.Fatalf("temperature lost: %v", a.Temperature)
	}
	if !strings.Contains(a.Prompt, "meticulous code reviewer") || !strings.Contains(a.Prompt, "Be concise.") {
		t.Fatalf("body/prompt lost: %q", a.Prompt)
	}
	// case-insensitive resolve.
	if _, ok := resolveAgent(dir, "REVIEWER"); !ok {
		t.Fatal("case-insensitive resolve failed")
	}
	// unknown + empty -> not resolved.
	if _, ok := resolveAgent(dir, "nope"); ok {
		t.Fatal("unknown agent should not resolve")
	}
	if _, ok := resolveAgent(dir, ""); ok {
		t.Fatal("empty agent should not resolve")
	}
}

func TestAgentToolAllowed(t *testing.T) {
	// nil policy: all allowed.
	if !(Agent{}).toolAllowed("write") {
		t.Fatal("nil policy should allow all")
	}
	a := Agent{Tools: map[string]bool{"write": false}}
	if a.toolAllowed("write") {
		t.Fatal("write should be denied")
	}
	if !a.toolAllowed("read") {
		t.Fatal("unlisted tool should be allowed")
	}
}

func TestLoadAgentsEmptyWhenNoDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := loadAgents(filepath.Join(t.TempDir(), "nonexistent")); len(got) != 0 {
		t.Fatalf("expected empty, got %d", len(got))
	}
}
