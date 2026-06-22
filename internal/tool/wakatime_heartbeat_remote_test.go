package tool

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestWakatimeHeartbeatRemote(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	os.Setenv("WAKATIME_API_KEY", "key")
	os.Setenv("WAKATIME_BASE_URL", ts.URL)
	defer func() {
		os.Unsetenv("WAKATIME_API_KEY")
		os.Unsetenv("WAKATIME_BASE_URL")
	}()

	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	r := NewDefaultRegistry()
	in := map[string]any{"project": "proj", "entity": "file.go", "language": "go", "send": true}
	res, err := runTool(t, r, "wakatime_heartbeat", sb, in)
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	var out struct {
		Accepted bool   `json:"accepted"`
		DryRun   bool   `json:"dryRun"`
		Target   string `json:"target"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Accepted || out.DryRun {
		t.Fatalf("expected accepted true and dryRun false, got accepted=%v dryRun=%v", out.Accepted, out.DryRun)
	}
	if out.Target != "wakatime" {
		t.Fatalf("unexpected target %s", out.Target)
	}
}
