package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSkillIndex(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, skillsSubdir, "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: demo-skill\ndescription: A demo skill for testing the index.\n---\n# Demo\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := loadSkillIndex(dir)
	if !strings.Contains(idx, "demo-skill") {
		t.Fatalf("index missing skill name: %q", idx)
	}
	if !strings.Contains(idx, "A demo skill for testing") {
		t.Fatalf("index missing description: %q", idx)
	}
	if !strings.Contains(idx, "<available_skills>") {
		t.Fatalf("index missing wrapper: %q", idx)
	}
	// Absent dir → empty.
	if got := loadSkillIndex(filepath.Join(dir, "nonexistent")); got != "" {
		t.Fatalf("expected empty index for missing dir, got %q", got)
	}
}
