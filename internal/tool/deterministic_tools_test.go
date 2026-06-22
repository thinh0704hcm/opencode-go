package tool

import (
	"strings"
	"testing"
)

func TestDeterministicTools(t *testing.T) {
	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New sandbox: %v", err)
	}
	r := NewDefaultRegistry()

	// type_inject
	out, err := runTool(t, r, "type_inject", sb, map[string]string{"code": "let x = 1;", "symbol": "x"})
	if err != nil {
		t.Fatalf("type_inject error: %v", err)
	}
	if !strings.Contains(out.Output, "type for symbol 'x'") {
		t.Errorf("type_inject output unexpected: %s", out.Output)
	}

	// svelte_assist
	out, err = runTool(t, r, "svelte_assist", sb, map[string]string{"component": "<script></script>", "goal": "basic"})
	if err != nil {
		t.Fatalf("svelte_assist error: %v", err)
	}
	if !strings.Contains(out.Output, "Svelte assistance checklist") {
		t.Errorf("svelte_assist output unexpected: %s", out.Output)
	}

	// plannotator
	out, err = runTool(t, r, "plannotator", sb, map[string]string{"plan": "Step one\nTODO: step two", "status": "ongoing"})
	if err != nil {
		t.Fatalf("plannotator error: %v", err)
	}
	if !strings.Contains(out.Output, "risk: high") && !strings.Contains(out.Output, "high") {
		t.Errorf("plannotator should flag high risk for TODO: %s", out.Output)
	}

	// speckit_chain
	out, err = runTool(t, r, "speckit_chain", sb, map[string]string{"spec": "some spec"})
	if err != nil {
		t.Fatalf("speckit_chain error: %v", err)
	}
	if !strings.Contains(out.Output, "clarify -> design") {
		t.Errorf("speckit_chain output unexpected: %s", out.Output)
	}

	// conductor
	tasks := []string{"build", "review code", "test"}
	out, err = runTool(t, r, "conductor", sb, map[string]any{"tasks": tasks})
	if err != nil {
		t.Fatalf("conductor error: %v", err)
	}
	if !strings.Contains(out.Output, "Execution order") {
		t.Errorf("conductor missing execution order header: %s", out.Output)
	}
	if !strings.Contains(out.Output, "[dependent]") {
		t.Errorf("conductor missing dependent classification: %s", out.Output)
	}

	// oh_my_opencode
	out, err = runTool(t, r, "oh_my_opencode", sb, map[string]string{})
	if err != nil {
		t.Fatalf("oh_my_opencode error: %v", err)
	}
	if !strings.Contains(out.Output, "read") {
		t.Errorf("oh_my_opencode output missing expected tool: %s", out.Output)
	}
	// cap errors test for type_inject and conductor
	// type_inject oversized code
	largeCode := strings.Repeat("a", (1<<20)+1)
	_, err = runTool(t, r, "type_inject", sb, map[string]string{"code": largeCode})
	if err == nil {
		t.Errorf("expected error for oversized code input")
	}
	// conductor oversized task
	largeTask := strings.Repeat("b", (8*1024)+1)
	_, err = runTool(t, r, "conductor", sb, map[string]any{"tasks": []string{largeTask}})
	if err == nil {
		t.Errorf("expected error for oversized task input")
	}
}
