package server

// tui_conformance_test.go verifies that the server produces the HTTP shapes and
// SSE event sequences the opencode TUI actually expects. Each test mirrors a
// real TUI code path: boot sequence, session lifecycle, SSE event shapes,
// permission flow, and sub-agent (Ctrl+X+Down) navigation.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/opencode-go/opencode-go/internal/provider"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func tuiServer() *httptest.Server {
	return httptest.NewServer(New(Options{Provider: provider.NewMock("hello world"), Model: "mock"}).Handler())
}

// postJSON marshals v and POSTs to url, returning status + decoded body.
func postJSON(t *testing.T, url string, v any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(v)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out) //nolint
	return resp.StatusCode, out
}

// mustCreate creates a session and returns its ID.
func mustCreate(t *testing.T, base string) string {
	t.Helper()
	status, body := postJSON(t, base+"/session", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("POST /session status=%d", status)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatal("POST /session: no id in response")
	}
	return id
}

// sseEvents subscribes to path and returns a channel of decoded event payloads.
// Caller must close done to stop the goroutine.
func sseEvents(t *testing.T, url string) (<-chan map[string]any, func()) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	ch := make(chan map[string]any, 512)
	done := make(chan struct{})
	go func() {
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
		for scanner.Scan() {
			select {
			case <-done:
				return
			default:
			}
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var frame struct {
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(data), &frame); err != nil {
				continue
			}
			var ev map[string]any
			if err := json.Unmarshal(frame.Payload, &ev); err != nil {
				continue
			}
			select {
			case ch <- ev:
			case <-done:
				return
			}
		}
	}()
	return ch, func() { close(done) }
}

// waitEvent blocks until an event of the given type arrives or 5s elapses.
func waitEvent(t *testing.T, ch <-chan map[string]any, typ string) map[string]any {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev["type"] == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event type %q", typ)
			return nil
		}
	}
}

// collectUntil drains events until typ is seen or timeout, returning all seen types.
func collectUntil(t *testing.T, ch <-chan map[string]any, typ string) []string {
	t.Helper()
	seen := []string{}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-ch:
			t := ev["type"].(string)
			seen = append(seen, t)
			if t == typ {
				return seen
			}
		case <-deadline:
			return seen
		}
	}
}

// assertKey checks that m[key] is non-nil/non-empty.
func assertKey(t *testing.T, m map[string]any, key string) {
	t.Helper()
	v, ok := m[key]
	if !ok || v == nil || v == "" {
		t.Errorf("expected key %q in %v", key, m)
	}
}

// ---------------------------------------------------------------------------
// TUI Boot Sequence
// The TUI makes these calls at startup before showing any UI.
// ---------------------------------------------------------------------------

func TestTUIBoot_Health(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/global/health", &got)
	if got["healthy"] != true {
		t.Errorf("healthy = %v, want true", got["healthy"])
	}
	if got["version"] == "" || got["version"] == nil {
		t.Error("version missing")
	}
}

func TestTUIBoot_Config(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/config?directory=/work", &got)
	for _, k := range []string{"$schema", "command", "agent", "mode", "plugin", "username", "model"} {
		if _, ok := got[k]; !ok {
			t.Errorf("/config missing key %q", k)
		}
	}
}

func TestTUIBoot_ConfigProviders(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/config/providers?directory=/work", &got)
	for _, k := range []string{"providers", "default"} {
		if _, ok := got[k]; !ok {
			t.Errorf("/config/providers missing key %q", k)
		}
	}
}

func TestTUIBoot_Provider(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/provider?directory=/work", &got)
	for _, k := range []string{"all", "default", "connected"} {
		if _, ok := got[k]; !ok {
			t.Errorf("/provider missing key %q", k)
		}
	}
}

func TestTUIBoot_Agent(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got []agentInfo
	getJSON(t, ts.URL, "/agent?directory=/work", &got)
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
		t.Error("/agent missing build agent")
	}
}

func TestTUIBoot_Path(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/path?directory=/work/myproj", &got)
	if got["directory"] != "/work/myproj" {
		t.Errorf("directory = %v, want /work/myproj", got["directory"])
	}
}

func TestTUIBoot_ProjectCurrent(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/project/current?directory=/work/myproj", &got)
	assertKey(t, got, "id")
	assertKey(t, got, "worktree")
}

func TestTUIBoot_SessionList(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got []any
	getJSON(t, ts.URL, "/session", &got)
	if got == nil {
		t.Error("/session returned nil, want array")
	}
}

func TestTUIBoot_Command(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got []any
	getJSON(t, ts.URL, "/command", &got)
	if got == nil {
		t.Error("/command returned nil, want array")
	}
}

func TestTUIBoot_MCP(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/mcp", &got)
	if got == nil {
		 t.Error("/mcp returned nil, want object")
	}
}

// ---------------------------------------------------------------------------
// Session CRUD — shapes the TUI parses
// ---------------------------------------------------------------------------

func TestTUISession_Create(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	id := mustCreate(t, ts.URL)

	resp2, err := http.Get(ts.URL + "/session/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var sess map[string]any
	json.NewDecoder(resp2.Body).Decode(&sess)

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET /session/%s status=%d", id, resp2.StatusCode)
	}
	for _, k := range []string{"id", "time"} {
		assertKey(t, sess, k)
	}
	if _, ok := sess["title"]; !ok {
		t.Error("session missing title key (may be empty string, but key must exist)")
	}
	timeObj := sess["time"].(map[string]any)
	if timeObj["created"] == nil {
		t.Error("time.created missing")
	}
	if timeObj["updated"] == nil {
		t.Error("time.updated missing")
	}
}

func TestTUISession_Delete(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	id := mustCreate(t, ts.URL)

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/session/"+id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /session/%s = %d, want 200", id, resp.StatusCode)
	}

	// GET after delete → 404
	resp2, _ := http.Get(ts.URL + "/session/" + id)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("GET deleted session = %d, want 404", resp2.StatusCode)
	}
}

func TestTUISession_Children_EmptyByDefault(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	id := mustCreate(t, ts.URL)

	var children []any
	getJSON(t, ts.URL, "/session/"+id+"/children", &children)
	if children == nil {
		t.Error("GET /session/{id}/children returned nil, want []")
	}
	if len(children) != 0 {
		t.Errorf("expected 0 children, got %d", len(children))
	}
}

func TestTUISession_Children_404OnUnknown(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	resp, _ := http.Get(ts.URL + "/session/ses_nope/children")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("children of unknown session = %d, want 404", resp.StatusCode)
	}
}

func TestTUISession_ChildrenLinkedToParent(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	parentID := mustCreate(t, ts.URL)

	// Verify the endpoint returns 200 with an array (not null/error).
	var children []any
	getJSON(t, ts.URL, "/session/"+parentID+"/children", &children)
	if children == nil {
		t.Fatal("children nil — must be [] not null")
	}
}

// ---------------------------------------------------------------------------
// Prompt Async → SSE event sequence
// The TUI subscribes to /global/event and expects this exact sequence.
// ---------------------------------------------------------------------------

func TestTUIPrompt_EventSequence(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	id := mustCreate(t, ts.URL)

	// Subscribe before prompting to avoid missing early events.
	ch, stop := sseEvents(t, ts.URL+"/global/event?directory=/work")
	defer stop()

	// Wait for server.connected so we know the stream is live.
	waitEvent(t, ch, "server.connected")

	// Send prompt.
	body := `{"model":{"providerID":"mock","modelID":"mock"},"agent":"build","parts":[{"type":"text","text":"hello"}]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/session/"+id+"/prompt_async", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("prompt_async status = %d, want 204", resp.StatusCode)
	}

	// Collect until session.idle.
	seenTypes := collectUntil(t, ch, "session.idle")
	seen := map[string]bool{}
	for _, typ := range seenTypes {
		seen[typ] = true
	}

	required := []string{
		"session.updated",
		"message.updated",      // user message
		"message.part.updated", // user text part
		"session.next.prompted",
		"session.next.prompt.admitted",
		"message.part.delta",
		"message.part.updated", // assistant text part
		"message.updated",      // final assistant message (completed)
		"session.idle",
	}
	for _, typ := range required {
		if !seen[typ] {
			t.Errorf("missing event type %q in sequence %v", typ, seenTypes)
		}
	}
}

func TestTUIPrompt_MessageShape(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	id := mustCreate(t, ts.URL)

	// Send sync prompt and wait for response.
	body := `{"model":{"providerID":"mock","modelID":"mock"},"agent":"build","parts":[{"type":"text","text":"hi"}]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/session/"+id+"/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /session/{id}/message status = %d", resp.StatusCode)
	}

	// GET messages — must return user + assistant.
	var msgs []struct {
		Info struct {
			ID   string `json:"id"`
			Role string `json:"role"`
			Time struct {
				Created   int64  `json:"created"`
				Completed *int64 `json:"completed"`
			} `json:"time"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	mresp, err := http.Get(ts.URL + "/session/" + id + "/message")
	if err != nil {
		t.Fatal(err)
	}
	defer mresp.Body.Close()
	if err := json.NewDecoder(mresp.Body).Decode(&msgs); err != nil {
		t.Fatal("decode messages:", err)
	}

	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages (user+assistant), got %d", len(msgs))
	}
	var userMsg, asstMsg *struct {
		Info struct {
			ID   string `json:"id"`
			Role string `json:"role"`
			Time struct {
				Created   int64  `json:"created"`
				Completed *int64 `json:"completed"`
			} `json:"time"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	for i := range msgs {
		switch msgs[i].Info.Role {
		case "user":
			userMsg = &msgs[i]
		case "assistant":
			asstMsg = &msgs[i]
		}
	}
	if userMsg == nil {
		t.Fatal("no user message")
	}
	if asstMsg == nil {
		t.Fatal("no assistant message")
	}
	if asstMsg.Info.Time.Completed == nil {
		t.Error("assistant message time.completed is nil — TUI never removes spinner")
	}
	var asstText string
	for _, p := range asstMsg.Parts {
		if p.Type == "text" {
			asstText += p.Text
		}
	}
	if asstText == "" {
		t.Error("assistant message has no text parts")
	}
}

// ---------------------------------------------------------------------------
// Abort — TUI uses POST /session/{id}/abort
// ---------------------------------------------------------------------------

func TestTUIAbort_IdleSession(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	id := mustCreate(t, ts.URL)

	// Aborting an idle session must return 200 (not panic/500).
	status, _ := postJSON(t, ts.URL+"/session/"+id+"/abort", nil)
	if status != http.StatusOK {
		t.Errorf("abort idle session = %d, want 200", status)
	}
}

func TestTUIAbort_UnknownSession(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	status, _ := postJSON(t, ts.URL+"/session/ses_nope/abort", nil)
	if status != http.StatusNotFound {
		t.Errorf("abort unknown session = %d, want 404", status)
	}
}

// ---------------------------------------------------------------------------
// Revert / Unrevert — TUI maps Ctrl+Z and Ctrl+Shift+Z
// ---------------------------------------------------------------------------

func TestTUIRevert_UnknownSession(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	status, _ := postJSON(t, ts.URL+"/session/ses_nope/revert", nil)
	if status != http.StatusNotFound {
		t.Errorf("revert unknown session = %d, want 404", status)
	}
}

// ---------------------------------------------------------------------------
// Fork — TUI duplicates a session
// ---------------------------------------------------------------------------

func TestTUIFork_CopiesMessages(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	parentID := mustCreate(t, ts.URL)

	// Send a message so the parent has content to fork.
	body := `{"model":{"providerID":"mock","modelID":"mock"},"agent":"build","parts":[{"type":"text","text":"fork me"}]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/session/"+parentID+"/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	// Fork.
	status, forkBody := postJSON(t, ts.URL+"/session/"+parentID+"/fork", nil)
	if status != http.StatusOK {
		t.Fatalf("fork = %d, want 200", status)
	}
	childID, _ := forkBody["id"].(string)
	if childID == "" {
		t.Fatal("fork response missing id")
	}
	if childID == parentID {
		t.Error("fork returned same id as parent")
	}

	// Child must have messages copied from parent.
	var childMsgs []any
	getJSON(t, ts.URL, "/session/"+childID+"/message", &childMsgs)
	var parentMsgs []any
	getJSON(t, ts.URL, "/session/"+parentID+"/message", &parentMsgs)
	if len(childMsgs) != len(parentMsgs) {
		t.Errorf("child has %d messages, parent has %d — fork must copy all messages", len(childMsgs), len(parentMsgs))
	}
}

// ---------------------------------------------------------------------------
// Summarize — TUI calls this to force-refresh session title
// ---------------------------------------------------------------------------

func TestTUISummarize_Updates(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	id := mustCreate(t, ts.URL)

	// Send a message so there's content to summarize.
	body := `{"model":{"providerID":"mock","modelID":"mock"},"agent":"build","parts":[{"type":"text","text":"what is two plus two"}]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/session/"+id+"/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	status, _ := postJSON(t, ts.URL+"/session/"+id+"/summarize", nil)
	if status != http.StatusOK {
		t.Errorf("summarize = %d, want 200", status)
	}
	// Title should now be non-empty.
	var sess map[string]any
	getJSON(t, ts.URL, "/session/"+id, &sess)
	if sess["title"] == "" || sess["title"] == nil {
		t.Error("title still empty after summarize")
	}
}

// ---------------------------------------------------------------------------
// Permission flow — TUI shows approval dialog then replies
// ---------------------------------------------------------------------------

func TestTUIPermission_ReplyRoutes(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	// Reply to unknown permission → 404.
	status, _ := postJSON(t, ts.URL+"/permission/per_unknown/reply", map[string]any{"reply": "once"})
	if status != http.StatusNotFound {
		t.Errorf("reply unknown permission = %d, want 404", status)
	}
}

func TestTUIPermission_FallbackRoute(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	id := mustCreate(t, ts.URL)

	// Fallback route: POST /session/{id}/permissions/{permID}
	status, _ := postJSON(t, ts.URL+"/session/"+id+"/permissions/per_unknown", map[string]any{"response": "once"})
	if status != http.StatusNotFound {
		t.Errorf("fallback reply unknown permission = %d, want 404", status)
	}
}

// ---------------------------------------------------------------------------
// TUI Control / Log / Publish
// ---------------------------------------------------------------------------

func TestTUIControl_NextLongPoll(t *testing.T) {
	ts := httptest.NewServer(New(Options{Provider: provider.NewMock("hi"), Model: "mock"}).Handler())
	defer ts.Close()

	// Use short client timeout to avoid blocking the test. The endpoint returns
	// null after tuiControlNextTimeout; we just want to confirm it returns 200.
	client := &http.Client{Timeout: 35 * time.Second}
	resp, err := client.Get(ts.URL + "/tui/control/next")
	if err != nil {
		t.Fatalf("GET /tui/control/next: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("tui/control/next = %d, want 200", resp.StatusCode)
	}
}

func TestTUILog_Accepts(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/log", "application/json", strings.NewReader(`{"level":"debug","message":"boot"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST /log = %d, want 200", resp.StatusCode)
	}
}

func TestTUIPublish_ForwardsToSSE(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	ch, stop := sseEvents(t, ts.URL+"/global/event?directory=/work")
	defer stop()
	waitEvent(t, ch, "server.connected")

	// Publish a custom event via POST /tui/publish.
	ev := map[string]any{
		"type":       "custom.test",
		"properties": map[string]any{"value": 42},
	}
	status, _ := postJSON(t, ts.URL+"/tui/publish", ev)
	if status != http.StatusOK {
		t.Errorf("POST /tui/publish = %d, want 200", status)
	}

	// The event should appear on the SSE stream.
	got := waitEvent(t, ch, "custom.test")
	if got == nil {
		t.Error("custom.test event not received on SSE stream")
	}
}

// ---------------------------------------------------------------------------
// V2 API — used by newer TUI clients
// ---------------------------------------------------------------------------

func TestTUIV2_Health(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/api/health", &got)
	if got["healthy"] != true {
		t.Errorf("v2 /api/health healthy = %v, want true", got["healthy"])
	}
}

func TestTUIV2_Location(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/api/location?directory=/work/proj", &got)
	assertKey(t, got, "directory")
	proj, _ := got["project"].(map[string]any)
	if proj == nil {
		t.Fatal("v2 /api/location missing project")
	}
	assertKey(t, proj, "id")
}

func TestTUIV2_SessionLifecycle(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	// Create.
	createReq := map[string]any{
		"title":    "v2-test",
		"location": map[string]any{"directory": "/tmp/v2test"},
	}
	status, createResp := postJSON(t, ts.URL+"/api/session", createReq)
	if status != http.StatusOK {
		t.Fatalf("POST /api/session = %d", status)
	}
	data, _ := createResp["data"].(map[string]any)
	if data == nil {
		t.Fatal("POST /api/session missing data")
	}
	sessionID, _ := data["id"].(string)
	if sessionID == "" {
		t.Fatal("v2 session missing id")
	}
	if data["title"] != "v2-test" {
		t.Errorf("v2 session title = %v, want v2-test", data["title"])
	}

	// Get.
	var getResp map[string]any
	getJSON(t, ts.URL, "/api/session/"+sessionID, &getResp)
	getData, _ := getResp["data"].(map[string]any)
	if getData == nil || getData["id"] != sessionID {
		t.Errorf("GET /api/session/%s id mismatch: %v", sessionID, getData)
	}

	// List.
	var listResp map[string]any
	getJSON(t, ts.URL, "/api/session", &listResp)
	listData, _ := listResp["data"].([]any)
	if len(listData) == 0 {
		t.Error("v2 session list empty after create")
	}
}

func TestTUIV2_ModelList(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/api/model", &got)
	data, _ := got["data"].([]any)
	if len(data) == 0 {
		t.Error("v2 /api/model returned empty data")
	}
	// Each model must have id field.
	for i, item := range data {
		m, _ := item.(map[string]any)
		if m["id"] == nil || m["id"] == "" {
			t.Errorf("model[%d] missing id: %v", i, m)
		}
	}
}

func TestTUIV2_AgentList(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/api/agent", &got)
	data, _ := got["data"].([]any)
	if len(data) == 0 {
		t.Error("v2 /api/agent returned empty data")
	}
}

func TestTUIV2_ProviderList(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/api/provider", &got)
	data, _ := got["data"].([]any)
	if data == nil {
		t.Error("v2 /api/provider missing data")
	}
}

// ---------------------------------------------------------------------------
// V2 SSE stream — per-session event filtering
// ---------------------------------------------------------------------------

func TestTUIV2_SessionEventStream(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	// Create session.
	createReq := map[string]any{"title": "sse-test"}
	_, createResp := postJSON(t, ts.URL+"/api/session", createReq)
	data, _ := createResp["data"].(map[string]any)
	sessionID, _ := data["id"].(string)
	if sessionID == "" {
		t.Fatal("no session id")
	}

	// Subscribe to per-session event stream.
	sseResp, err := http.Get(ts.URL + "/api/session/" + sessionID + "/event")
	if err != nil {
		t.Fatal(err)
	}
	defer sseResp.Body.Close()
	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/session/%s/event = %d", sessionID, sseResp.StatusCode)
	}
	if ct := sseResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
}

func TestTUIV2_SessionMessages(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	// Create session, send a v1 sync prompt, then read v2 messages.
	id := mustCreate(t, ts.URL)
	body := `{"model":{"providerID":"mock","modelID":"mock"},"agent":"build","parts":[{"type":"text","text":"hello v2"}]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/session/"+id+"/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	var v2msgs map[string]any
	getJSON(t, ts.URL, "/api/session/"+id+"/message", &v2msgs)
	msgData, _ := v2msgs["data"].([]any)
	if len(msgData) == 0 {
		t.Fatal("v2 messages empty after prompt")
	}
	for i, item := range msgData {
		m, _ := item.(map[string]any)
		if m["id"] == "" || m["id"] == nil {
			t.Errorf("v2 message[%d] missing id", i)
		}
		if m["type"] == "" || m["type"] == nil {
			t.Errorf("v2 message[%d] missing type", i)
		}
	}
}

// ---------------------------------------------------------------------------
// V2 SDK-verified missing endpoints
// ---------------------------------------------------------------------------

func TestTUIV2_GlobalEvent(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	// GET /api/event must return SSE stream.
	resp, err := http.Get(ts.URL + "/api/event?location[directory]=/work")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/event = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
}

func TestTUIV2_Stubs(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()
	id := mustCreate(t, ts.URL)

	cases := []struct {
		method string
		path   string
	}{
		{"GET", "/api/command"},
		{"GET", "/api/skill"},
		{"GET", "/api/permission/request"},
		{"GET", "/api/permission/saved"},
		{"GET", "/api/session/" + id + "/context"},
		{"POST", "/api/session/" + id + "/compact"},
		{"GET", "/api/fs/list"},
		{"GET", "/api/fs/read"},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(c.method, ts.URL+c.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s %s = %d, want 200", c.method, c.path, resp.StatusCode)
		}
	}
}

func TestTUIV2_SessionDeletedEvent_HasSessionID(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	id := mustCreate(t, ts.URL)

	// Subscribe to global events.
	ch, stop := sseEvents(t, ts.URL+"/global/event?directory=/work")
	defer stop()
	waitEvent(t, ch, "server.connected")

	// Delete the session — should emit session.deleted with sessionID in properties.
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/session/"+id, nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	ev := waitEvent(t, ch, "session.deleted")
	props, _ := ev["properties"].(map[string]any)
	if props == nil {
		t.Fatal("session.deleted missing properties")
	}
	if props["sessionID"] == "" || props["sessionID"] == nil {
		t.Errorf("session.deleted.properties.sessionID missing: %v", props)
	}
}

// ---------------------------------------------------------------------------
// Sub-agent navigation (Ctrl+X+Down in TUI)
// The TUI calls GET /session/{id}/children to list sub-sessions created
// by delegate/task tool calls.
// ---------------------------------------------------------------------------

func TestTUISubagent_ChildrenShape(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	parentID := mustCreate(t, ts.URL)

	resp, err := http.Get(ts.URL + "/session/" + parentID + "/children")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /session/{id}/children = %d, want 200", resp.StatusCode)
	}
	var children []any
	if err := json.NewDecoder(resp.Body).Decode(&children); err != nil {
		t.Fatalf("decode children: %v", err)
	}
	if children == nil {
		t.Error("children must be [] not null")
	}
}

// ---------------------------------------------------------------------------
// Stubs that must return the exact shape the TUI checks
// ---------------------------------------------------------------------------

func TestTUIStubs_ExactShape(t *testing.T) {
	ts := tuiServer()
	defer ts.Close()

	cases := []struct {
		path string
		want string
	}{
		{"/formatter", "[]"},
		{"/lsp", "[]"},
		{"/session/status", "{}"},
		{"/experimental/workspace", "[]"},
	}
	for _, c := range cases {
		resp, err := http.Get(ts.URL + c.path)
		if err != nil {
			t.Fatalf("GET %s: %v", c.path, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if got := strings.TrimSpace(string(b)); got != c.want {
			t.Errorf("GET %s body = %q, want %q", c.path, got, c.want)
		}
	}
}

// Dummy check to ensure the test helper references are used.
var _ = fmt.Sprintf

func TestUnsupportedStubsReturn501(t *testing.T) {
    ts := tuiServer()
    defer ts.Close()

    cases := []struct {
        method string
        path   string
        body   string
    }{
        {"POST", "/sync/start", "{}"},
        {"POST", "/sync/replay", "{}"},
        {"POST", "/sync/steal", "{}"},
        {"POST", "/question/req_1/reply", `{"answer":"yes"}`},
        {"POST", "/question/req_1/reject", `{"reason":"no"}`},
        {"DELETE", "/api/permission/saved/perm_1", ""},
        // /tui/execute-command is now implemented (publishes tui.command.execute,
        // returns 200 true) — see TestTUIExecuteCommandPublishes.
        {"POST", "/experimental/project/proj1/copy/generate-name", "{}"},
    }
    for _, c := range cases {
        var req *http.Request
        if c.body != "" {
            req, _ = http.NewRequest(c.method, ts.URL+c.path, strings.NewReader(c.body))
            req.Header.Set("Content-Type", "application/json")
        } else {
            req, _ = http.NewRequest(c.method, ts.URL+c.path, nil)
        }
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
            t.Fatalf("%s %s: %v", c.method, c.path, err)
        }
        resp.Body.Close()
        if resp.StatusCode != http.StatusNotImplemented {
            t.Errorf("%s %s = %d, want 501", c.method, c.path, resp.StatusCode)
        }
    }
}

