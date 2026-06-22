package tool

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestMDTableFormatter(t *testing.T) {
	tmp := t.TempDir()
	sb, err := New(tmp)
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	r := NewDefaultRegistry()
	input := map[string]string{"text": "| Name|Age|\n|Alice|30|\n|Bob|  5|"}
	// runTool expects *Sandbox; use helper.
	res, err := runTool(t, r, "md_table_formatter", sb, input)
	if err != nil {
		t.Fatalf("md_table_formatter error: %v", err)
	}
	expected := "| Name  | Age |\n| Alice | 30  |\n| Bob   | 5   |"
	if res.Output != expected {
		t.Fatalf("output mismatch: got %q, want %q", res.Output, expected)
	}
	// Ensure sandbox not used; just for API compliance.
	_ = sb
	_ = context.Background()
	_ = json.RawMessage{}
	_ = os.Stdin
}
