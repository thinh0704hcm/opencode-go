package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/tool"
)

// startScripted boots a server backed by the scripted tool provider over a
// fresh DefaultRegistry rooted at workdir, returning the httptest server.
func startScripted(t *testing.T, calls []provider.ToolCall, finalText, workdir string) *httptest.Server {
	t.Helper()
	srv := New(Options{
		Provider: provider.NewScriptedToolProvider(calls, finalText),
		Model:    "scripted",
		Tools:    tool.NewDefaultRegistry(),
		Workdir:  workdir,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// subscribeEvents opens GET /global/event?directory=/work and returns a channel
// of decoded sseFrame envelopes (reusing sseFrame from server_test.go).
func subscribeEvents(t *testing.T, body *http.Response) <-chan sseFrame {
	t.Helper()
	events := make(chan sseFrame, 256)
	go func() {
		scanner := bufio.NewScanner(body.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var f sseFrame
			if err := json.Unmarshal([]byte(data), &f); err != nil {
				continue
			}
			events <- f
		}
	}()
	return events
}

// promptAsync POSTs prompt_async and asserts 204.
func promptAsync(t *testing.T, baseURL, sessionID string) {
	t.Helper()
	body := `{"model":{"providerID":"scripted","modelID":"scripted"},"agent":"build","parts":[{"type":"text","text":"go"}]}`
	status, _ := doRequest(t, http.MethodPost, baseURL+"/session/"+sessionID+"/prompt_async", body)
	if status != http.StatusNoContent {
		t.Fatalf("prompt_async status = %d, want 204", status)
	}
}

// loopResult captures what the SSE stream revealed about one agent-loop run.
type loopResult struct {
	toolParts    map[string]bool
	textBuf      string
	sawIdle      bool
	sawPermAsked bool
	permID       string
}

// collectUntilIdle drains events until session.idle. When reply != "" and a
// permission.asked arrives it POSTs that reply to /permission/{id}/reply so the
// gated loop can proceed. Bounded by a 10s deadline so a hang fails fast.
func collectUntilIdle(t *testing.T, events <-chan sseFrame, sessionID, baseURL, reply string) loopResult {
	t.Helper()
	res := loopResult{toolParts: map[string]bool{}}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case f := <-events:
			var pe struct {
				Type       string          `json:"type"`
				Properties json.RawMessage `json:"properties"`
			}
			json.Unmarshal(f.Payload, &pe)
			switch pe.Type {
			case "message.part.delta":
				var p struct {
					SessionID string `json:"sessionID"`
					Field     string `json:"field"`
					Delta     string `json:"delta"`
				}
				json.Unmarshal(pe.Properties, &p)
				if p.SessionID == sessionID && p.Field == "text" {
					res.textBuf += p.Delta
				}
			case "message.part.updated":
				var p struct {
					SessionID string `json:"sessionID"`
					Part      struct {
						Type string `json:"type"`
						Tool string `json:"tool"`
					} `json:"part"`
				}
				json.Unmarshal(pe.Properties, &p)
				if p.SessionID == sessionID && p.Part.Type == "tool" {
					res.toolParts[p.Part.Tool] = true
				}
			case "permission.asked":
				var p struct {
					ID        string `json:"id"`
					SessionID string `json:"sessionID"`
				}
				json.Unmarshal(pe.Properties, &p)
				res.sawPermAsked = true
				res.permID = p.ID
				if reply != "" {
					status, _ := doRequest(t, http.MethodPost, baseURL+"/permission/"+p.ID+"/reply", `{"reply":"`+reply+`"}`)
					if status != http.StatusOK {
						t.Fatalf("permission reply status = %d, want 200", status)
					}
				}
			case "session.idle":
				res.sawIdle = true
				return res
			}
		case <-deadline:
			t.Fatal("timed out waiting for session.idle")
		}
	}
}

// runScenario boots a scripted server rooted at workdir, subscribes, prompts,
// and returns the collected loop result (replying to any permission with reply).
func runScenario(t *testing.T, calls []provider.ToolCall, finalText, workdir, reply string) loopResult {
	t.Helper()
	ts := startScripted(t, calls, finalText, workdir)
	sid := createSession(t, ts.URL)
	stream, err := http.Get(ts.URL + "/global/event?directory=/work")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	events := subscribeEvents(t, stream)
	waitForType(t, events, "server.connected", "/work")
	promptAsync(t, ts.URL, sid)
	return collectUntilIdle(t, events, sid, ts.URL, reply)
}

// TestAgentLoopExecutesReadOnlyTool proves a read-only tool runs WITHOUT a
// permission prompt and the loop completes with the scripted final text.
func TestAgentLoopExecutesReadOnlyTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := []provider.ToolCall{{ID: "c1", Name: "read", Input: json.RawMessage(`{"path":"note.txt"}`)}}
	res := runScenario(t, calls, "done reading", dir, "")

	if !res.toolParts["read"] {
		t.Error("missing message.part.updated tool part for read")
	}
	if res.sawPermAsked {
		t.Error("read is read-only; no permission.asked expected")
	}
	if strings.TrimSpace(res.textBuf) != "done reading" {
		t.Errorf("final text = %q, want %q", strings.TrimSpace(res.textBuf), "done reading")
	}
	if !res.sawIdle {
		t.Error("missing session.idle")
	}
}

// TestAgentLoopGatesMutatingToolAllow proves an "once" reply lets the mutating
// tool execute: the file is created with the scripted content and the loop
// completes.
func TestAgentLoopGatesMutatingToolAllow(t *testing.T) {
	dir := t.TempDir()
	calls := []provider.ToolCall{{ID: "c3", Name: "write", Input: json.RawMessage(`{"path":"out.txt","content":"x"}`)}}
	res := runScenario(t, calls, "after", dir, "once")

	if !res.sawPermAsked {
		t.Fatal("write is mutating; expected permission.asked")
	}
	got, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatalf("out.txt should exist after allow: %v", err)
	}
	if string(got) != "x" {
		t.Errorf("out.txt content = %q, want %q", string(got), "x")
	}
	if !res.sawIdle {
		t.Error("missing session.idle")
	}
}

// TestAgentLoopGatesMutatingToolReject proves a mutating tool prompts for
// permission, and a reject reply means the file is NOT written yet the loop
// still completes with the scripted final text.
func TestAgentLoopGatesMutatingToolReject(t *testing.T) {
	dir := t.TempDir()
	calls := []provider.ToolCall{{ID: "c2", Name: "write", Input: json.RawMessage(`{"path":"out.txt","content":"x"}`)}}
	res := runScenario(t, calls, "after", dir, "reject")

	if !res.sawPermAsked {
		t.Fatal("write is mutating; expected permission.asked")
	}
	if res.permID == "" {
		t.Error("permission.asked missing id")
	}
	if _, err := os.Stat(filepath.Join(dir, "out.txt")); !os.IsNotExist(err) {
		t.Errorf("out.txt should not exist after reject, stat err = %v", err)
	}
	if strings.TrimSpace(res.textBuf) != "after" {
		t.Errorf("final text = %q, want %q", strings.TrimSpace(res.textBuf), "after")
	}
	if !res.sawIdle {
		t.Error("missing session.idle")
	}
}
