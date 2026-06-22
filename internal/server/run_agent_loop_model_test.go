//go:build opencode_wip

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/tool"
)

type modelRecordingMock struct {
	mu      sync.Mutex
	lastReq provider.ChatRequest
}

func (m *modelRecordingMock) ID() string { return "mock" }
func (m *modelRecordingMock) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatChunk, error) {
	m.mu.Lock()
	m.lastReq = req
	m.mu.Unlock()
	out := make(chan provider.ChatChunk, 1)
	go func() {
		defer close(out)
		out <- provider.ChatChunk{TextDelta: "ok", FinishReason: "stop"}
	}()
	return out, nil
}

func TestRunAgentLoopModelNormalization(t *testing.T) {
	mock := &modelRecordingMock{}
	srv := New(Options{Provider: mock, Model: "mock", Tools: tool.NewDefaultRegistry()})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	sessionID := createSession(t, ts.URL)
	// POST synchronous message (not async) to trigger runAgentLoop directly.
	body := `{"model":{"providerID":"concactao","modelID":"openai/gpt-5.5"},"parts":[{"type":"text","text":"test"}]}`
	status, _ := doRequest(t, http.MethodPost, ts.URL+"/session/"+sessionID+"/message", body)
	if status != http.StatusOK && status != http.StatusNoContent && status != http.StatusCreated {
		t.Fatalf("unexpected status %d", status)
	}

	mock.mu.Lock()
	model := mock.lastReq.Model
	mock.mu.Unlock()

	if model != "cx/gpt-5.5" {
		t.Fatalf("expected model 'cx/gpt-5.5', got %q", model)
	}
}
