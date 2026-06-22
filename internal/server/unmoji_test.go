//go:build opencode_wip

package server

import "testing"

func TestUnmoji(t *testing.T) {
	cases := []struct {
		in, out string
	}{{"Hello 🌍!", "Hello !"}, {"Just text", "Just text"}}
	for _, c := range cases {
		got := Unmoji(c.in)
		if got != c.out {
			t.Fatalf("Unmoji(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}
