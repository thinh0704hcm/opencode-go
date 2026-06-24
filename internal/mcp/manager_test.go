package mcp

import "testing"

func TestManagerConnectsLocalServer(t *testing.T) {
	py := skipIfNoPython(t)
	section := map[string]any{
		"mock": map[string]any{
			"type":    "local",
			"command": []any{py, "-c", mockServerScript},
		},
	}
	m := NewManager(section)
	defer m.Shutdown()

	st := m.Status()
	if len(st) != 1 || st[0].Status != "connected" || st[0].ToolCount != 1 {
		t.Fatalf("status = %+v", st)
	}
	ads := m.Adapters()
	if len(ads) != 1 || ads[0].Name() != "mock_echo" {
		t.Fatalf("adapters = %v", ads)
	}
}

func TestManagerHandlesBadAndDisabled(t *testing.T) {
	section := map[string]any{
		"disabled": map[string]any{"type": "local", "command": []any{"x"}, "enabled": false},
		"remote":   map[string]any{"type": "remote", "url": "https://x"},
		"nocmd":    map[string]any{"type": "local"},
	}
	m := NewManager(section)
	defer m.Shutdown()
	byName := map[string]ServerStatus{}
	for _, s := range m.Status() {
		byName[s.Name] = s
	}
	if byName["disabled"].Status != "disabled" {
		t.Fatalf("disabled = %+v", byName["disabled"])
	}
	if byName["remote"].Status != "error" {
		t.Fatalf("remote = %+v", byName["remote"])
	}
	if byName["nocmd"].Status != "error" {
		t.Fatalf("nocmd = %+v", byName["nocmd"])
	}
	if len(m.Adapters()) != 0 {
		t.Fatal("no adapters expected")
	}
}
