package server

import "testing"

func TestComputeCost(t *testing.T) {
	// claude-opus: 1M in @15 + 1M out @75 = 90.
	if got := computeCost("kr/claude-opus-4.8-thinking-agentic", 1_000_000, 1_000_000); got != 90.0 {
		t.Fatalf("opus cost = %v, want 90", got)
	}
	// substring match through provider prefix.
	if got := computeCost("cx/gpt-5.5-review", 1_000_000, 0); got != 1.25 {
		t.Fatalf("gpt-5 input cost = %v, want 1.25", got)
	}
	// unknown model -> 0.
	if got := computeCost("totally-unknown-model", 1_000_000, 1_000_000); got != 0 {
		t.Fatalf("unknown cost = %v, want 0", got)
	}
	// empty -> 0.
	if got := computeCost("", 100, 100); got != 0 {
		t.Fatalf("empty model cost = %v, want 0", got)
	}
}
