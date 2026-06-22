package tool

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Helper to run a tool with given input map.
func runToolRemote(t *testing.T, r *Registry, name string, sb *Sandbox, in map[string]any) Result {
	t.Helper()
	res, err := runTool(t, r, name, sb, in)
	if err != nil {
		t.Fatalf("run %s: %v", name, err)
	}
	return res
}

func TestMemoryRemoteAdd(t *testing.T) {
	var received struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		Scope string `json:"scope"`
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/memories" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	os.Setenv("SUPERMEMORY_API_KEY", "test-key")
	os.Setenv("SUPERMEMORY_BASE_URL", ts.URL)
	defer func() {
		os.Unsetenv("SUPERMEMORY_API_KEY")
		os.Unsetenv("SUPERMEMORY_BASE_URL")
	}()

	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	r := NewDefaultRegistry()
	res := runToolRemote(t, r, "memory", sb, map[string]any{"action": "add", "scope": "project", "key": "k", "value": "v", "remote": true})
	if res.Output != "added" {
		t.Fatalf("expected added, got %s", res.Output)
	}
	if received.Key != "k" || received.Value != "v" || received.Scope != "project" {
		t.Fatalf("unexpected payload: %+v", received)
	}
}

func TestMemoryRemoteList(t *testing.T) {
	body := []byte(`[{"key":"a","value":"1"},{"key":"b","value":"2"}]`)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/memories" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer ts.Close()

	os.Setenv("SUPERMEMORY_API_KEY", "test-key")
	os.Setenv("SUPERMEMORY_BASE_URL", ts.URL)
	defer func() {
		os.Unsetenv("SUPERMEMORY_API_KEY")
		os.Unsetenv("SUPERMEMORY_BASE_URL")
	}()

	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	r := NewDefaultRegistry()
	res := runToolRemote(t, r, "memory", sb, map[string]any{"action": "list", "scope": "project", "limit": 10, "remote": true})
	if res.Output != string(body) {
		t.Fatalf("list output mismatch: got %s", res.Output)
	}
}
