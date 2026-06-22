//go:build opencode_wip

package server

import (
	"bytes"
	"os"
	"testing"
)

func TestVibeGuardRedactRestore(t *testing.T) {
	// Enable guard via env var
	_ = os.Setenv("VIBE_GUARD_ENABLED", "true")
	vg := NewVibeGuard()
	input := []byte("api key sk-ABCDEF12345678 and Bearer abcdefghijklmnop")
	redacted, meta := vg.Redact(input)
	if bytes.Contains(redacted, []byte("sk-")) {
		t.Fatalf("redacted output still contains secret")
	}
	restored := vg.Restore(redacted, meta)
	if string(restored) != string(input) {
		t.Fatalf("restore mismatch: got %s want %s", restored, input)
	}
	// Disabled guard passes through
	_ = os.Setenv("VIBE_GUARD_ENABLED", "false")
	vg2 := NewVibeGuard()
	unchanged, meta2 := vg2.Redact(input)
	if string(unchanged) != string(input) {
		t.Fatalf("disabled guard should not modify input")
	}
	if len(meta2) != 0 {
		t.Fatalf("disabled guard meta should be empty")
	}
}
