package tool

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserSessionStatusUsesGETWithoutURL(t *testing.T) {
	var gotMethod, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			r.Body.Read(b)
		}
		gotBody = string(b)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()
	t.Setenv("BROWSER_CONTROL_ENDPOINT", ts.URL)

	res, err := runToolWithTempSandbox(t, "browser_session", map[string]any{"action": "status"})
	if err != nil {
		t.Fatalf("browser_session status: %v", err)
	}
	if res.Output != "ok" || gotMethod != http.MethodGet || gotBody != "" {
		t.Fatalf("unexpected status call output=%q method=%s body=%q", res.Output, gotMethod, gotBody)
	}
}

func TestBrowserSessionNavigateValidatesURLAndPayload(t *testing.T) {
	var payload map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Write([]byte("nav"))
	}))
	defer ts.Close()
	t.Setenv("BROWSER_CONTROL_ENDPOINT", ts.URL)

	_, err := runToolWithTempSandbox(t, "browser_session", map[string]any{"action": "navigate", "url": "ftp://bad"})
	if err == nil {
		t.Fatalf("expected invalid url error")
	}
	res, err := runToolWithTempSandbox(t, "browser_session", map[string]any{"action": "navigate", "url": "https://example.com/a"})
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if res.Output != "nav" || payload["action"] != "navigate" || payload["url"] != "https://example.com/a" {
		t.Fatalf("unexpected payload/output %#v %q", payload, res.Output)
	}
}

func TestBrowserSessionPayloadFields(t *testing.T) {
	var payload map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.Write([]byte("ok"))
	}))
	defer ts.Close()
	t.Setenv("BROWSER_CONTROL_ENDPOINT", ts.URL)

	x, y := 11, 22
	res, err := runToolWithTempSandbox(t, "browser_session", map[string]any{
		"action": "click", "selector": "#go", "x": x, "y": y, "text": "hello", "script": "return 1", "session_id": "s1",
	})
	if err != nil {
		t.Fatalf("click: %v", err)
	}
	if res.Output != "ok" || payload["selector"] != "#go" || payload["text"] != "hello" || payload["script"] != "return 1" || payload["session_id"] != "s1" || payload["x"].(float64) != 11 || payload["y"].(float64) != 22 {
		t.Fatalf("unexpected click payload %#v output=%q", payload, res.Output)
	}

	_, err = runToolWithTempSandbox(t, "browser_session", map[string]any{"action": "type", "text": strings.Repeat("x", 8193)})
	if err == nil {
		t.Fatalf("expected text size error")
	}
	_, err = runToolWithTempSandbox(t, "browser_session", map[string]any{"action": "eval", "script": strings.Repeat("x", 65537)})
	if err == nil {
		t.Fatalf("expected script size error")
	}
}

func TestBrowserSessionInvalidEndpointRejected(t *testing.T) {
	t.Setenv("BROWSER_CONTROL_ENDPOINT", "ftp://bad")
	_, err := runToolWithTempSandbox(t, "browser_session", map[string]any{"action": "status"})
	if err == nil {
		t.Fatalf("expected invalid endpoint error")
	}
}

func TestBrowserSessionNon2xxStatusOnlyNoBodyLeak(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "secret body", http.StatusTeapot)
	}))
	defer ts.Close()
	t.Setenv("BROWSER_CONTROL_ENDPOINT", ts.URL)

	_, err := runToolWithTempSandbox(t, "browser_session", map[string]any{"action": "text"})
	if err == nil {
		t.Fatalf("expected endpoint error")
	}
	if strings.Contains(err.Error(), "secret body") || !strings.Contains(err.Error(), "418") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func runToolWithTempSandbox(t *testing.T, name string, in any) (Result, error) {
	t.Helper()
	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	return runTool(t, NewDefaultRegistry(), name, sb, in)
}
