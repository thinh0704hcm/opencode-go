// Blocked: depends on gated normalize.go provider/model normalizer.
//go:build opencode_wip

package server

import (
	"testing"
)

func TestNormalizeProviderModel_New(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		wantP    string
		wantM    string
	}{
		{"openrouter", "openai/gpt-4o", "openrouter", "openai/gpt-4o"},
		{"concactao", "cx/test", "concactao", "cx/test"},
		{"openai", "gpt-4o", "concactao", "gpt-4o"},
		{"openai", "gpt-5.5", "concactao", "cx/gpt-5.5"},
		{"", "openrouter/openai/gpt-4o", "openrouter", "openai/gpt-4o"},
		{"concactao", "openai/gpt-5.5", "concactao", "cx/gpt-5.5"},
		{"openai", "openai/gpt-5.5", "concactao", "cx/gpt-5.5"},
		{"openai", "gpt-5.5", "concactao", "cx/gpt-5.5"},
		{"", "openai/gpt-5.5", "concactao", "cx/gpt-5.5"},
	}
	for _, tt := range tests {
		p, m := NormalizeProviderModel(tt.provider, tt.model)
		if p != tt.wantP || m != tt.wantM {
			t.Errorf("NormalizeProviderModel(%q, %q) = %q, %q; want %q, %q", tt.provider, tt.model, p, m, tt.wantP, tt.wantM)
		}
	}
}
