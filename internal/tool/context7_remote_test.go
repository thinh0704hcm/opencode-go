package tool

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestContext7RemoteFetch(t *testing.T) {
	respBody := []byte("remote content")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/docs" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write(respBody)
	}))
	defer ts.Close()

	os.Setenv("CONTEXT7_BASE_URL", ts.URL)
	defer os.Unsetenv("CONTEXT7_BASE_URL")

	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	r := NewDefaultRegistry()
	res, err := runTool(t, r, "context7", sb, map[string]any{"package": "pkg", "query": "q", "version": "v1", "remote": true})
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	var out struct {
		URL     string `json:"url"`
		Content string `json:"content"`
		Mode    string `json:"mode"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Mode != "remote" {
		t.Fatalf("expected mode remote, got %s", out.Mode)
	}
	if out.Content != string(respBody) {
		t.Fatalf("content mismatch: %s", out.Content)
	}
}
