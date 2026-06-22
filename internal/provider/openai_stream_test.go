package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// helper to collect chunks
func collectChunksStream(t *testing.T, ch <-chan ChatChunk) []ChatChunk {
	t.Helper()
	var out []ChatChunk
	for cc := range ch {
		out = append(out, cc)
	}
	return out
}

func TestOpenAIStreamPrematureEOF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		// send a content chunk without finish_reason
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		flusher.Flush()
		// close without [DONE]
	}))
	defer srv.Close()

	p := NewOpenAI("test", srv.URL, "", "m", srv.Client())
	ch, err := p.StreamChat(context.Background(), ChatRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("StreamChat error: %v", err)
	}
	chunks := collectChunksStream(t, ch)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	if chunks[0].TextDelta != "hi" {
		t.Errorf("first chunk text = %q, want \"hi\"", chunks[0].TextDelta)
	}
	if chunks[len(chunks)-1].Err == nil {
		t.Errorf("expected final error chunk, got nil")
	}
}

func TestOpenAIStreamFinishReasonStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		// send with finish_reason stop
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n"))
		flusher.Flush()
		// close without [DONE]
	}))
	defer srv.Close()

	p := NewOpenAI("test", srv.URL, "", "m", srv.Client())
	ch, err := p.StreamChat(context.Background(), ChatRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("StreamChat error: %v", err)
	}
	chunks := collectChunksStream(t, ch)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].FinishReason != "stop" {
		t.Errorf("finish reason = %q, want \"stop\"", chunks[0].FinishReason)
	}
	if chunks[0].Err != nil {
		t.Errorf("unexpected error chunk: %v", chunks[0].Err)
	}
}

func TestOpenAIStreamDoneMarker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewOpenAI("test", srv.URL, "", "m", srv.Client())
	ch, err := p.StreamChat(context.Background(), ChatRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("StreamChat error: %v", err)
	}
	chunks := collectChunksStream(t, ch)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].TextDelta != "hi" {
		t.Errorf("text = %q, want \"hi\"", chunks[0].TextDelta)
	}
	if chunks[1].FinishReason != "stop" {
		t.Errorf("expected final stop chunk, got %v", chunks[1])
	}

}
