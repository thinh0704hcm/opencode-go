package tool

import (
	"testing"
)

func TestMyStatus(t *testing.T) {
	r := NewDefaultRegistry()
	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	// No input needed.
	res, err := runTool(t, r, "mystatus", sb, map[string]string{})
	if err != nil {
		t.Fatalf("mystatus error: %v", err)
	}
	if res.Output == "" {
		t.Fatalf("mystatus output empty")
	}
}
