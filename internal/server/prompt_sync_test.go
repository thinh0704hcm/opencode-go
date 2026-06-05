package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/opencode-go/opencode-go/internal/provider"
)

// TestPromptSyncReturnsAssistant verifies POST /session/{id}/message blocks
// until the assistant turn completes and returns 200 with the final assistant
// {info, parts}: info.role == "assistant" and a non-empty text part.
func TestPromptSyncReturnsAssistant(t *testing.T) {
	srv := New(Options{Provider: provider.NewMock("hello world"), Model: "mock"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createSession(t, ts.URL)

	body := `{"model":{"providerID":"mock","modelID":"mock"},"agent":"build","parts":[{"type":"text","text":"hi"}]}`
	status, raw := doRequest(t, http.MethodPost, ts.URL+"/session/"+id+"/message", body)
	if status != http.StatusOK {
		t.Fatalf("POST message status = %d, want 200 (body=%q)", status, string(raw))
	}

	var got struct {
		Info struct {
			Role string `json:"role"`
			Time struct {
				Completed *int64 `json:"completed"`
			} `json:"time"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v (body=%q)", err, string(raw))
	}

	if got.Info.Role != "assistant" {
		t.Fatalf("info.role = %q, want assistant", got.Info.Role)
	}
	if got.Info.Time.Completed == nil {
		t.Fatal("info.time.completed is nil, want set (turn must be complete)")
	}

	var text string
	for _, p := range got.Parts {
		if p.Type == "text" {
			text += p.Text
		}
	}
	if text != "hello world" {
		t.Fatalf("assistant text = %q, want %q", text, "hello world")
	}
}

// TestPromptSyncUnknownSession404 verifies POST to an unknown session is 404.
func TestPromptSyncUnknownSession404(t *testing.T) {
	srv := New(Options{Provider: provider.NewMock("hi"), Model: "mock"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"model":{"providerID":"mock","modelID":"mock"},"parts":[{"type":"text","text":"hi"}]}`
	status, _ := doRequest(t, http.MethodPost, ts.URL+"/session/ses_nope/message", body)
	if status != http.StatusNotFound {
		t.Fatalf("POST message unknown status = %d, want 404", status)
	}
}

// TestPromptSyncStillStreams verifies the synchronous endpoint emits the same
// SSE event sequence (delta + final message.updated + session.idle) so the TUI
// and bot still observe streaming while the HTTP call blocks.
func TestPromptSyncStillStreams(t *testing.T) {
	srv := New(Options{Provider: provider.NewMock("stream me"), Model: "mock"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createSession(t, ts.URL)

	streamResp, err := http.Get(ts.URL + "/global/event?directory=/work")
	if err != nil {
		t.Fatal(err)
	}
	defer streamResp.Body.Close()

	// Collect events in the background.
	events := make(chan sseFrame, 256)
	go func() {
		scanner := bufio.NewScanner(streamResp.Body)
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

	waitForType(t, events, "server.connected", "/work")

	// Fire the synchronous prompt; it blocks until the turn finishes.
	body := `{"model":{"providerID":"mock","modelID":"mock"},"parts":[{"type":"text","text":"hi"}]}`
	status, _ := doRequest(t, http.MethodPost, ts.URL+"/session/"+id+"/message", body)
	if status != http.StatusOK {
		t.Fatalf("POST message status = %d, want 200", status)
	}

	// Assert the streaming event sequence: delta + final message.updated + session.idle.
	sawDelta := false
	sawUpdated := false
	sawFinalMsg := false
	sawIdle := false
	deadline := time.After(5 * time.Second)
loop:
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
				}
				json.Unmarshal(pe.Properties, &p)
				if p.Field == "text" && p.SessionID == id {
					sawDelta = true
				}
			case "message.part.updated":
				var p struct {
					SessionID string `json:"sessionID"`
					Part      struct {
						Type string `json:"type"`
					} `json:"part"`
				}
				json.Unmarshal(pe.Properties, &p)
				if p.Part.Type == "text" && p.SessionID == id {
					sawUpdated = true
				}
			case "message.updated":
				var p struct {
					Info struct {
						Role string `json:"role"`
						Time struct {
							Completed *int64 `json:"completed"`
						} `json:"time"`
					} `json:"info"`
				}
				json.Unmarshal(pe.Properties, &p)
				if p.Info.Role == "assistant" && p.Info.Time.Completed != nil {
					sawFinalMsg = true
				}
			case "session.idle":
				sawIdle = true
				break loop
			}
		case <-deadline:
			t.Fatal("timed out waiting for event sequence")
		}
	}

	if !sawDelta {
		t.Error("missing message.part.delta (field text)")
	}
	if !sawUpdated {
		t.Error("missing message.part.updated (part text)")
	}
	if !sawFinalMsg {
		t.Error("missing final assistant message.updated with time.completed")
	}
	if !sawIdle {
		t.Error("missing synthetic session.idle")
	}
}
