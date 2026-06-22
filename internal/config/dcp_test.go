package config

import "testing"

func TestDCPConfigDefaults(t *testing.T) {
	cfg := Config{Raw: map[string]any{}}
	d := cfg.DCP()
	if d.Enabled {
		t.Error("expected Enabled false default")
	}
	if d.Mode != "range" {
		t.Errorf("expected default Mode 'range', got %s", d.Mode)
	}
	if d.ErrorPruneTurns != 4 {
		t.Errorf("expected default ErrorPruneTurns 4, got %d", d.ErrorPruneTurns)
	}
	if d.TurnNudgeInterval != 5 {
		t.Errorf("expected default TurnNudgeInterval 5, got %d", d.TurnNudgeInterval)
	}
}

func TestDCPConfigParsing(t *testing.T) {
	cfg := Config{Raw: map[string]any{"dcp": map[string]any{
		"enabled":             true,
		"mode":                "compact",
		"protectUserMessages": true,
		"protectedTools":      []any{"tool1", "tool2"},
		"errorPruneTurns":     10.0,
		"turnNudgeInterval":   2.0,
		"manualMode":          true,
	}}}
	d := cfg.DCP()
	if !d.Enabled {
		t.Error("Enabled should be true")
	}
	if d.Mode != "compact" {
		t.Errorf("Mode got %s", d.Mode)
	}
	if !d.ProtectUserMessages {
		t.Error("ProtectUserMessages true expected")
	}
	if len(d.ProtectedTools) != 2 || d.ProtectedTools[0] != "tool1" || d.ProtectedTools[1] != "tool2" {
		t.Errorf("ProtectedTools mismatch: %+v", d.ProtectedTools)
	}
	if d.ErrorPruneTurns != 10 {
		t.Errorf("ErrorPruneTurns %d", d.ErrorPruneTurns)
	}
	if d.TurnNudgeInterval != 2 {
		t.Errorf("TurnNudgeInterval %d", d.TurnNudgeInterval)
	}
	if !d.ManualMode {
		t.Error("ManualMode true expected")
	}
}
