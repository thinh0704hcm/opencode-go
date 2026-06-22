//go:build opencode_wip

package server

import (
	"testing"
)

func TestNormalizeProviderModel(t *testing.T) {
	tests := []struct {
		providerID string
		modelID    string
		expProv    string
		expModel   string
	}{
		{"openai", "gpt-5.5", "concactao", "cx/gpt-5.5"},
		{"anthropic", "claude-opus-4-6-thinking", "concactao", "ag/claude-opus-4-6-thinking"},
		{"concactao", "gpt-5.5", "concactao", "cx/gpt-5.5"},
		{"concactao", "gpt-5.5-review", "concactao", "cx/gpt-5.5-review"},
		{"openai", "gpt-5.5", "concactao", "cx/gpt-5.5"},
		{"concactao", "gpt-5.5-xhigh", "concactao", "gpt-5.5-xhigh"},
		{"concactao", "claude-opus-4-6-thinking", "concactao", "ag/claude-opus-4-6-thinking"},
		{"concactao", "claude-opus-4-6-thinking-xhigh", "concactao", "claude-opus-4-6-thinking-xhigh"},
		{"", "cx/gpt-5.5", "concactao", "cx/gpt-5.5"},
		{"", "ag/claude-opus-4-6-thinking", "concactao", "ag/claude-opus-4-6-thinking"},
		{"", "cc/gpt-4o", "concactao", "cc/gpt-4o"},
		{"", "openai/gpt-4o", "concactao", "gpt-4o"},
	}

	for _, tt := range tests {
		p, m := NormalizeProviderModel(tt.providerID, tt.modelID)
		if p != tt.expProv || m != tt.expModel {
			t.Errorf("Normalize(%s, %s) = %s, %s; want %s, %s", tt.providerID, tt.modelID, p, m, tt.expProv, tt.expModel)
		}
	}
}

func TestToolSchemaSubagentType(t *testing.T) {
	// Delegate and task are handled by schemaForTool (builtin), NOT the registry
	s := schemaForTool("delegate")
	params := s.Parameters["properties"].(map[string]any)
	if _, ok := params["subagent_type"]; !ok {
		t.Error("subagent_type not found in delegate schema")
	}

	s = schemaForTool("task")
	params = s.Parameters["properties"].(map[string]any)
	if _, ok := params["subagent_type"]; !ok {
		t.Error("subagent_type not found in task schema")
	}
}
