package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWarpGrepBasic(t *testing.T) {
	tmp := t.TempDir()
	sb, err := New(tmp)
	if err != nil {
		t.Fatalf("sandbox New: %v", err)
	}
	r := NewDefaultRegistry()
	r.Register(NewWarpGrepTool())

	// create files
	os.WriteFile(filepath.Join(tmp, "file1.txt"), []byte("apple\nbanana\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "file2.txt"), []byte("cat\napple\n"), 0o644)

	in := map[string]any{"pattern": "apple"}
	res, err := runTool(t, r, "warpgrep", sb, in)
	if err != nil {
		t.Fatalf("warpgrep exec: %v", err)
	}
	if !strings.Contains(res.Output, "file1.txt:1:apple") || !strings.Contains(res.Output, "file2.txt:2:apple") {
		t.Fatalf("warpgrep output missing expected lines: %s", res.Output)
	}
}

func TestWarpGrepLimits(t *testing.T) {
	tmp := t.TempDir()
	sb, _ := New(tmp)
	r := NewDefaultRegistry()
	r.Register(NewWarpGrepTool())
	// many matches
	for i := 0; i < 5; i++ {
		fname := fmt.Sprintf("f%d.txt", i)
		os.WriteFile(filepath.Join(tmp, fname), []byte("a\n"), 0o644)
	}
	// limit matches to 2
	in := map[string]any{"pattern": "a", "maxMatches": 2}
	res, err := runTool(t, r, "warpgrep", sb, in)
	if err != nil {
		t.Fatalf("warpgrep exec: %v", err)
	}
	count := strings.Count(res.Output, "a")
	if count != 2 {
		t.Fatalf("expected 2 matches, got %d", count)
	}
}
