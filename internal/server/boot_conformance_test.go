package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// bootTestDir is a synthetic worktree threaded through ?directory= so /path and
// /project/current have deterministic, assertable values. directoryParam echoes
// the query value verbatim, so the path need not exist on disk.
const bootTestDir = "/work/conformance-proj"

// dirQuery returns "?directory=<bootTestDir>" url-encoded.
func dirQuery() string {
	return "?directory=" + url.QueryEscape(bootTestDir)
}

// getRaw issues GET base+path, asserts HTTP 200, and returns the trimmed body.
func getRaw(t *testing.T, base, path string) string {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", path, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s read: %v", path, err)
	}
	return strings.TrimSpace(string(b))
}

// getJSON issues GET base+path, asserts HTTP 200, and decodes into out.
func getJSON(t *testing.T, base, path string, out any) {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("GET %s decode: %v", path, err)
	}
}

func TestConfigShape(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	var got map[string]json.RawMessage
	getJSON(t, ts.URL, "/config"+dirQuery(), &got)

	for _, k := range []string{"$schema", "command", "agent", "mode", "plugin", "username", "model"} {
		if _, ok := got[k]; !ok {
			t.Errorf("/config missing key %q", k)
		}
	}
}

func TestConfigProvidersShape(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	var got map[string]json.RawMessage
	getJSON(t, ts.URL, "/config/providers"+dirQuery(), &got)

	for _, k := range []string{"providers", "default"} {
		if _, ok := got[k]; !ok {
			t.Errorf("/config/providers missing key %q", k)
		}
	}
}

func TestProviderShape(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	var got map[string]json.RawMessage
	getJSON(t, ts.URL, "/provider"+dirQuery(), &got)

	for _, k := range []string{"all", "default", "connected"} {
		if _, ok := got[k]; !ok {
			t.Errorf("/provider missing key %q", k)
		}
	}
}

func TestAgentShape(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	var got []agentInfo
	getJSON(t, ts.URL, "/agent"+dirQuery(), &got)

	if len(got) == 0 {
		t.Fatal("/agent returned empty array")
	}
	found := false
	for _, a := range got {
		if a.Name == "build" {
			found = true
		}
	}
	if !found {
		t.Errorf("/agent missing build agent: %+v", got)
	}
}

func TestPathShape(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	var got pathResponse
	getJSON(t, ts.URL, "/path"+dirQuery(), &got)

	if got.Directory != bootTestDir {
		t.Errorf("/path directory = %q, want %q", got.Directory, bootTestDir)
	}
	if got.Worktree != bootTestDir {
		t.Errorf("/path worktree = %q, want %q", got.Worktree, bootTestDir)
	}
}

func TestProjectCurrentShape(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	var got struct {
		ID       string `json:"id"`
		Worktree string `json:"worktree"`
	}
	getJSON(t, ts.URL, "/project/current"+dirQuery(), &got)

	if got.Worktree != bootTestDir {
		t.Errorf("/project/current worktree = %q, want %q", got.Worktree, bootTestDir)
	}
	if want := filepath.Base(bootTestDir); got.ID != want {
		t.Errorf("/project/current id = %q, want %q", got.ID, want)
	}
}

func TestEmptyStubShapes(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	cases := []struct {
		path string
		want string
	}{
		// /command now has a real config/file-backed shape; covered by behavior tests.
		// /mcp now has a real config-backed shape (M5b); covered by behavior tests.
		{"/formatter", "[]"},
		{"/lsp", "[]"},
		{"/session/status", "{}"},
		{"/experimental/workspace", "[]"},
	}
	for _, c := range cases {
		if got := getRaw(t, ts.URL, c.path); got != c.want {
			t.Errorf("%s body = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestBoot404Stubs(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	var gotDirs []map[string]any
	getJSON(t, ts.URL, "/project/test-id/directories", &gotDirs)
	if len(gotDirs) == 0 || gotDirs[0]["type"] != "main" {
		t.Errorf("/project/test-id/directories missing main dir: %v", gotDirs)
	}

	var gotRef map[string]any
	getJSON(t, ts.URL, "/api/reference", &gotRef)
	if _, ok := gotRef["data"]; !ok {
		t.Errorf("/api/reference missing data: %v", gotRef)
	}

	var gotInt map[string]any
	getJSON(t, ts.URL, "/api/integration", &gotInt)
	if _, ok := gotInt["data"]; !ok {
		t.Errorf("/api/integration missing data: %v", gotInt)
	}
}

func TestCommandEndpointReturnsArray(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	var got []any
	getJSON(t, ts.URL, "/command", &got)
	if got == nil {
		t.Fatal("/command returned nil, want array")
	}
}

func TestTUIControlNextReturns200(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	// The long-poll blocks up to tuiControlNextTimeout (25s) before returning
	// 200 with null in M2; bound the client just above that to detect a hang.
	client := &http.Client{Timeout: tuiControlNextTimeout + 5*time.Second}
	resp, err := client.Get(ts.URL + "/tui/control/next")
	if err != nil {
		t.Fatalf("GET /tui/control/next: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/tui/control/next status = %d, want 200", resp.StatusCode)
	}
}

func TestLogReturns200(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/log", "application/json", strings.NewReader(`{"level":"debug","message":"hi"}`))
	if err != nil {
		t.Fatalf("POST /log: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/log status = %d, want 200", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if got := strings.TrimSpace(string(b)); got != "{}" {
		t.Errorf("/log body = %q, want %q", got, "{}")
	}
}
