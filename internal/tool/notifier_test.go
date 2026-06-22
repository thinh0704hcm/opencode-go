package tool

import (
	"testing"
)

func TestNotifier(t *testing.T) {
	r := NewDefaultRegistry()
	tmp := t.TempDir()
	sb, err := New(tmp)
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	input := map[string]string{"title": "Test", "message": "hello"}
	res, err := runTool(t, r, "notifier", sb, input)
	if err != nil {
		t.Fatalf("notifier error: %v", err)
	}
	// Output may be "sent" or a fallback message; ensure non‑empty.
	if res.Output == "" {
		t.Fatalf("notifier output is empty")
	}
}
