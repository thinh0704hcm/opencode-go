package tool

import (
	"os"
	"strings"
	"testing"
)

func TestObservabilityStatus(t *testing.T) {
	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	r := NewDefaultRegistry()
	// set dummy env vars
	os.Setenv("HELICONE_API_KEY", "dummy")
	os.Setenv("SENTRY_DSN", "dummydsn")
	os.Setenv("WAKATIME_API_KEY", "dummywt")
	defer func() {
		os.Unsetenv("HELICONE_API_KEY")
		os.Unsetenv("HELICONE_BASE_URL")
		os.Unsetenv("SENTRY_DSN")
		os.Unsetenv("SENTRY_AUTH_TOKEN")
		os.Unsetenv("WAKATIME_API_KEY")
	}()
	out, err := runTool(t, r, "observability_status", sb, map[string]string{})
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	// Ensure no secret values appear in output
	if strings.Contains(out.Output, "dummy") {
		t.Errorf("output leaks env values: %s", out.Output)
	}
	// Check configured true fields present
	if !strings.Contains(out.Output, "\"configured\":true") {
		t.Errorf("expected configured:true in output: %s", out.Output)
	}
}

func TestWakatimeHeartbeat(t *testing.T) {
	sb, _ := New(t.TempDir())
	r := NewDefaultRegistry()
	in := map[string]string{"project": "myproj", "entity": "myfile.go", "language": "go"}
	out, err := runTool(t, r, "wakatime_heartbeat", sb, in)
	if err != nil {
		t.Fatalf("heartbeat error: %v", err)
	}
	if !strings.Contains(out.Output, "\"dryRun\":true") || !strings.Contains(out.Output, "\"target\":\"wakatime\"") {
		t.Errorf("heartbeat output missing fields: %s", out.Output)
	}
	// missing required field
	_, err = runTool(t, r, "wakatime_heartbeat", sb, map[string]string{"project": "p"})
	if err == nil {
		t.Errorf("expected error for missing entity")
	}
}

func TestMicodeDeterministic(t *testing.T) {
	sb, _ := New(t.TempDir())
	r := NewDefaultRegistry()
	in := map[string]string{"request": "do something useful"}
	out, err := runTool(t, r, "micode", sb, in)
	if err != nil {
		t.Fatalf("micode error: %v", err)
	}
	if !strings.Contains(out.Output, "Step 1") {
		t.Errorf("expected steps in output: %s", out.Output)
	}
	// oversize request
	big := strings.Repeat("a", (1<<20)+1)
	_, err = runTool(t, r, "micode", sb, map[string]string{"request": big})
	if err == nil {
		t.Errorf("expected error for oversized request")
	}
}

func TestOcttoMarkdown(t *testing.T) {
	sb, _ := New(t.TempDir())
	r := NewDefaultRegistry()
	in := map[string]any{"title": "Bug", "body": "Fix it", "labels": []string{"bug", "urgent"}}
	out, err := runTool(t, r, "octto", sb, in)
	if err != nil {
		t.Fatalf("octto error: %v", err)
	}
	if !strings.Contains(out.Output, "# Bug") || !strings.Contains(out.Output, "Fix it") || !strings.Contains(out.Output, "Labels: bug, urgent") {
		t.Errorf("octto output missing content: %s", out.Output)
	}
	// too many labels
	many := make([]string, 21)
	for i := range many {
		many[i] = "l"
	}
	_, err = runTool(t, r, "octto", sb, map[string]any{"title": "t", "body": "b", "labels": many})
	if err == nil {
		t.Errorf("expected error for too many labels")
	}
}

func TestRalphWiggumModes(t *testing.T) {
	sb, _ := New(t.TempDir())
	r := NewDefaultRegistry()
	// simple mode
	out, err := runTool(t, r, "ralph_wiggum", sb, map[string]string{"problem": "issue", "mode": "simple"})
	if err != nil {
		t.Fatalf("simple mode error: %v", err)
	}
	if !strings.Contains(out.Output, "Simplified:") {
		t.Errorf("expected simplified output: %s", out.Output)
	}
	// risks mode
	out, err = runTool(t, r, "ralph_wiggum", sb, map[string]string{"problem": "issue", "mode": "risks"})
	if err != nil {
		t.Fatalf("risks mode error: %v", err)
	}
	if !strings.Contains(out.Output, "Potential risks") {
		t.Errorf("expected risks output: %s", out.Output)
	}
	// default (no mode)
	out, err = runTool(t, r, "ralph_wiggum", sb, map[string]string{"problem": "issue"})
	if err != nil {
		t.Fatalf("default mode error: %v", err)
	}
	if !strings.Contains(out.Output, "Simplified:") || !strings.Contains(out.Output, "Potential risks") {
		t.Errorf("expected combined output: %s", out.Output)
	}
}
