package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	
	"github.com/opencode-go/opencode-go/internal/session"
)

func TestV2Health(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/api/health", &got)
	if got["healthy"] != true {
		t.Errorf("expected healthy: true, got %+v", got)
	}
}

func TestV2Location(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/api/location", &got)
	if got["directory"] == "" {
		t.Error("expected non-empty directory")
	}
	proj := got["project"].(map[string]any)
	if proj["id"] == "" {
		t.Error("expected non-empty project id")
	}
}

func TestV2SessionLifecycle(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	// 1. Create session
	req := map[string]any{
		"title": "Test Session",
		"location": map[string]any{
			"directory": "/tmp/test",
		},
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(ts.URL+"/api/session", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/session status = %d", resp.StatusCode)
	}
	var createResp map[string]any
	json.NewDecoder(resp.Body).Decode(&createResp)
	resp.Body.Close()

	data := createResp["data"].(map[string]any)
	sessionID := data["id"].(string)
	if data["title"] != "Test Session" {
		t.Errorf("expected title Test Session, got %q", data["title"])
	}

	// 2. Get session
	var getResp map[string]any
	getJSON(t, ts.URL, "/api/session/"+sessionID, &getResp)
	getData := getResp["data"].(map[string]any)
	if getData["id"] != sessionID {
		t.Errorf("expected id %s, got %s", sessionID, getData["id"])
	}

	// 3. List sessions
	var listResp map[string]any
	getJSON(t, ts.URL, "/api/session", &listResp)
	listData := listResp["data"].([]any)
	if len(listData) == 0 {
		t.Error("expected non-empty session list")
	}

	// 4. Prompt session (busy conflict)
	// We'll skip complex prompt testing here as it needs a mock provider/agent turn.
}

func TestV2AgentList(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/api/agent", &got)
	data := got["data"].([]any)
	if len(data) == 0 {
		t.Error("expected non-empty agent list")
	}
	a := data[0].(map[string]any)
	if req, ok := a["request"].(map[string]any); !ok || req["headers"] == nil {
		t.Errorf("expected request block with headers, got %v", a["request"])
	}
}

func TestV2ModelList(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/api/model", &got)
	data := got["data"].([]any)
	if len(data) == 0 {
		t.Error("expected non-empty model list")
	}
	m := data[0].(map[string]any)
	if costs, ok := m["cost"].([]any); !ok || len(costs) == 0 {
		t.Errorf("expected cost to be non-empty array, got %v", m["cost"])
	}
	if api, ok := m["api"].(map[string]any); !ok || api["type"] != "aisdk" {
		t.Errorf("expected valid api block, got %v", m["api"])
	}
	if _, ok := m["variants"].([]any); !ok {
		t.Errorf("expected variants array, got %v", m["variants"])
	}
	if status, ok := m["status"].(string); !ok || status != "active" {
		t.Errorf("expected status 'active', got %v", m["status"])
	}
}

func TestV2ProviderList(t *testing.T) {
	ts := httptest.NewServer(newTestServer().Handler())
	defer ts.Close()

	var got map[string]any
	getJSON(t, ts.URL, "/api/provider", &got)
	data := got["data"].([]any)
	if len(data) == 0 {
		t.Error("expected non-empty provider list")
	}
	p := data[0].(map[string]any)
	if api, ok := p["api"].(map[string]any); !ok || api["type"] != "aisdk" {
		t.Errorf("expected valid api block, got %v", p["api"])
	}
	if req, ok := p["request"].(map[string]any); !ok || req["headers"] == nil {
		t.Errorf("expected request block with headers, got %v", p["request"])
	}
}

func TestV2SessionPromptResume(t *testing.T) {
	srv := newTestServer()
	sid := "sess-resume"
	srv.store.CreateSessionWithID(sid, "", "proj1", "")
	
	// Create request correctly with httptest and mux
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	
	client := ts.Client()

	// Try resuming an empty session
	req := v2PromptRequest{Resume: true}
	b, _ := json.Marshal(req)
	res, err := client.Post(ts.URL+"/api/session/"+sid+"/prompt", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("Expected 400 for resume with no assistant msg, got %d", res.StatusCode)
	}

	// Setup an assistant message to resume
	srv.store.AppendUserMessage(sid, "msg1", "test-provider", "test-model", "test-agent", []string{"hello"})
	asst, _ := srv.store.NewAssistantMessage(sid, "msg1", "test-provider", "test-model", "test-agent", "build")

	res2, err := client.Post(ts.URL+"/api/session/"+sid+"/prompt", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()

	if res2.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", res2.StatusCode)
	}

	// Check that no extra user message was appended when resuming with empty prompt
	msgs, _ := srv.store.Messages(sid)
	if len(msgs) != 2 { // user msg + assistant msg
		t.Fatalf("Expected exactly 2 messages in store, got %d", len(msgs))
	}
	if msgs[len(msgs)-1].Info.ID != asst.Info.ID {
		t.Errorf("Expected latest message to remain the assistant msg")
	}
}

func TestV2MapSubtaskAndToolParts(t *testing.T) {
	srv := newTestServer()
	sid := "sess-map-test"
	srv.store.CreateSessionWithID(sid, "", "proj1", "")
	
	// Add user message
	srv.store.AppendUserMessage(sid, "", "mock", "model", "build", []string{"delegate something"})
	
	// Add assistant message
	asst, _ := srv.store.NewAssistantMessage(sid, "", "mock", "model", "build", "build", false)
	mid := asst.Info.ID

	// Append both subtask part and tool part for 'delegate'
	srv.store.AppendSubtaskPart(sid, mid, "do it", "", "researcher", "mock", "model", "target-123")
	srv.store.AppendToolPart(sid, mid, "delegate", "call-1", "running", map[string]any{"prompt": "do it"}, "")
	
	// Fetch session messages
	msgs, _ := srv.store.Messages(sid)
	var asstMsg interface{}
	for _, m := range msgs {
		if m.Info.Role == "assistant" {
			asstMsg = m
			break
		}
	}

	// Map to V2
	mapped := srv.mapToV2Message(asstMsg.(session.MessageWithParts)).(map[string]any)
	content := mapped["content"].([]any)
	
	// Verify content length is 2 (step-start + subtask), tool should be hidden
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %d: %v", len(content), content)
	}

	subtask := content[1].(map[string]any)
	if subtask["type"] != "subtask" {
		t.Errorf("expected type subtask, got %v", subtask["type"])
	}
	if subtask["prompt"] != "do it" {
		t.Errorf("expected prompt 'do it', got %v", subtask["prompt"])
	}
	if subtask["sessionID"] != "target-123" {
		t.Errorf("expected target sessionID, got %v", subtask["sessionID"])
	}
	
	// Verify user message shape
	userMsg := srv.mapToV2Message(msgs[0]).(map[string]any)
	if _, ok := userMsg["role"]; ok {
		t.Errorf("expected no role in user message")
	}
	if _, ok := userMsg["metadata"]; !ok {
		t.Errorf("expected metadata in user message")
	}
}
