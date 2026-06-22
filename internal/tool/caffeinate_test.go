package tool

import (
	"testing"
)

func TestCaffeinate(t *testing.T) {
	r := NewDefaultRegistry()
	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	input := map[string]int{"seconds": 5}
	res, err := runTool(t, r, "caffeinate", sb, input)
	if err != nil {
		t.Fatalf("caffeinate error: %v", err)
	}
	if res.Output == "" {
		t.Fatalf("caffeinate output empty")
	}
}
