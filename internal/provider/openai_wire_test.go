package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureServer records the JSON request body sent to /chat/completions, then
// returns a minimal valid SSE stream so StreamChat completes cleanly.
func captureServer(t *testing.T, into *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		*into = body
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
}

func drain(t *testing.T, ch <-chan ChatChunk) {
	t.Helper()
	for cc := range ch {
		if cc.Err != nil {
			t.Fatalf("unexpected error chunk: %v", cc.Err)
		}
	}
}

// TestOpenAIMaxTokensEmittedWhenPositive: a valid budget is sent verbatim
// (AI SDK parity: max_tokens = maxOutputTokens).
func TestOpenAIMaxTokensEmittedWhenPositive(t *testing.T) {
	var body map[string]any
	srv := captureServer(t, &body)
	defer srv.Close()

	p := NewOpenAI("test", srv.URL, "", "m", srv.Client())
	ch, err := p.StreamChat(context.Background(), ChatRequest{
		Model:     "m",
		Messages:  []ChatMessage{{Role: "user", Content: "hi"}},
		MaxTokens: 4096,
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	drain(t, ch)

	got, ok := body["max_tokens"]
	if !ok {
		t.Fatalf("max_tokens missing; want 4096")
	}
	if int(got.(float64)) != 4096 {
		t.Errorf("max_tokens = %v, want 4096", got)
	}
}

// TestOpenAIMaxTokensOmittedWhenInvalid: a < 1 budget (the bug that crashes the
// TS client) is dropped, not forwarded.
func TestOpenAIMaxTokensOmittedWhenInvalid(t *testing.T) {
	for _, mt := range []int{0, -1, -22402} {
		var body map[string]any
		srv := captureServer(t, &body)

		p := NewOpenAI("test", srv.URL, "", "m", srv.Client())
		ch, err := p.StreamChat(context.Background(), ChatRequest{
			Model:     "m",
			Messages:  []ChatMessage{{Role: "user", Content: "hi"}},
			MaxTokens: mt,
		})
		if err != nil {
			t.Fatalf("StreamChat: %v", err)
		}
		drain(t, ch)
		srv.Close()

		if _, ok := body["max_tokens"]; ok {
			t.Errorf("MaxTokens=%d: max_tokens should be omitted, got %v", mt, body["max_tokens"])
		}
	}
}

// TestOpenAIReasoningContentParity: by default reasoning_content on an assistant
// message is forwarded (AI SDK parity).
func TestOpenAIReasoningContentParity(t *testing.T) {
	var body map[string]any
	srv := captureServer(t, &body)
	defer srv.Close()

	p := NewOpenAI("test", srv.URL, "", "m", srv.Client())
	ch, err := p.StreamChat(context.Background(), ChatRequest{
		Model: "m",
		Messages: []ChatMessage{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ReasoningContent: "thinking hard"},
			{Role: "user", Content: "again"},
		},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	drain(t, ch)

	msgs := body["messages"].([]any)
	asst := msgs[1].(map[string]any)
	if asst["reasoning_content"] != "thinking hard" {
		t.Errorf("reasoning_content = %v, want %q", asst["reasoning_content"], "thinking hard")
	}
}
