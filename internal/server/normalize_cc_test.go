//go:build opencode_wip

package server

import (
	"testing"
)

func TestNormalizeProviderModel_CC(t *testing.T) {
	tests := []struct {
		providerID string
		modelID    string
		wantP      string
		wantM      string
	}{
		{"openai", "cc/claude-opus-4-6", "concactao", "cc/claude-opus-4-6"},
		{"concactao", "cc/model", "concactao", "cc/model"},
	}

	for _, tt := range tests {
		gotP, gotM := NormalizeProviderModel(tt.providerID, tt.modelID)
		if gotP != tt.wantP || gotM != tt.wantM {
			t.Errorf("NormalizeProviderModel(%q, %q) = %q, %q; want %q, %q", tt.providerID, tt.modelID, gotP, gotM, tt.wantP, tt.wantM)
		}
	}
}
