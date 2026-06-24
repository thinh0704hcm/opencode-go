// Blocked: depends on gated markdown_formatter.go table formatter.
//go:build opencode_wip

package server

import (
	"strings"
	"testing"
)

type row struct {
	ID   int
	Name string
}

func TestTableMarkdown(t *testing.T) {
	rows := []row{{1, "Bob"}, {2, "Eve"}}
	out := TableMarkdown(rows)
	if out == "" {
		t.Fatalf("expected non-empty output")
	}
	if !testContains(out, "ID|Name") || !testContains(out, "1|Bob") || !testContains(out, "2|Eve") {
		t.Fatalf("output missing expected content: %s", out)
	}
	if TableMarkdown([]row{}) != "" {
		t.Fatalf("empty slice should return empty string")
	}
}

func testContains(s, substr string) bool { return strings.Contains(s, substr) }
