package tool

import (
	"strings"
	"testing"
)

func TestFlashCompactTruncate(t *testing.T) {
	r := NewDefaultRegistry()
	r.Register(NewFlashCompactTool())
	sb, _ := New(t.TempDir())
	// multibyte string: three Chinese characters (9 bytes).
	input := map[string]any{"output": "世世世世世", "budget": 4}
	res, err := runTool(t, r, "flashcompact", sb, input)
	if err != nil {
		t.Fatalf("flashcompact exec: %v", err)
	}
	if !res.Truncated {
		t.Fatalf("expected Truncated true")
	}
	if !strings.HasSuffix(res.Output, "...(truncated)") {
		t.Fatalf("output missing truncation marker: %s", res.Output)
	}
}

func TestFlashCompactDefaultBudget(t *testing.T) {
	r := NewDefaultRegistry()
	r.Register(NewFlashCompactTool())
	sb, _ := New(t.TempDir())
	input := map[string]any{"output": "short"}
	res, err := runTool(t, r, "flashcompact", sb, input)
	if err != nil {
		t.Fatalf("flashcompact exec: %v", err)
	}
	if res.Truncated {
		t.Fatalf("expected no truncation for short output")
	}
	if res.Output != "short" {
		t.Fatalf("output changed: %s", res.Output)
	}
}
