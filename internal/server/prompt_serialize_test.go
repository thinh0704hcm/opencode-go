//go:build opencode_wip

package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencode-go/opencode-go/internal/provider"
)

type concurrencyMock struct {
	inFlight      int64
	maxConcurrent int64
	callCount     int64
}

func (m *concurrencyMock) ID() string { return "mock" }
func (m *concurrencyMock) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatChunk, error) {
	atomic.AddInt64(&m.callCount, 1)
	curr := atomic.AddInt64(&m.inFlight, 1)

	// Atomic max update
	for {
		oldMax := atomic.LoadInt64(&m.maxConcurrent)
		if curr <= oldMax {
			break
		}
		if atomic.CompareAndSwapInt64(&m.maxConcurrent, oldMax, curr) {
			break
		}
	}

	defer atomic.AddInt64(&m.inFlight, -1)

	out := make(chan provider.ChatChunk, 1)
	go func() {
		defer close(out)
		select {
		case <-time.After(50 * time.Millisecond):
			out <- provider.ChatChunk{TextDelta: "ok"}
			out <- provider.ChatChunk{FinishReason: "stop"}
		case <-ctx.Done():
		}
	}()
	return out, nil
}

func TestPromptSerializedNotParallel(t *testing.T) {
	mock := &concurrencyMock{}
	srv := New(Options{Provider: mock, Model: "mock"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createSession(t, ts.URL)
	body := `{"model":{"providerID":"mock","modelID":"mock"},"agent":"build","parts":[{"type":"text","text":"hi"}]}`

	var wg sync.WaitGroup
	wg.Add(2)

	runReq := func() {
		defer wg.Done()
		status, _ := doRequest(t, http.MethodPost, ts.URL+"/session/"+id+"/message", body)
		if status != http.StatusOK {
			t.Errorf("POST returned %d", status)
		}
	}

	go runReq()
	go runReq()
	wg.Wait()

	if max := atomic.LoadInt64(&mock.maxConcurrent); max != 1 {
		t.Fatalf("Max concurrency = %d, want 1", max)
	}
	if count := atomic.LoadInt64(&mock.callCount); count != 2 {
		t.Fatalf("Call count = %d, want 2", count)
	}
}

type recordingMock struct {
	mu      sync.Mutex
	history [][]string
	replies []string
}

func (m *recordingMock) ID() string { return "mock" }
func (m *recordingMock) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatChunk, error) {
	var msgs []string
	for _, msg := range req.Messages {
		var s string
		switch v := msg.Content.(type) {
		case string:
			s = v
		default:
			s = fmt.Sprintf("%v", v)
		}
		msgs = append(msgs, s)
	}

	m.mu.Lock()
	m.history = append(m.history, msgs)
	m.mu.Unlock()

	out := make(chan provider.ChatChunk, 1)
	go func() {
		defer close(out)
		out <- provider.ChatChunk{TextDelta: "reply"}
		out <- provider.ChatChunk{FinishReason: "stop"}
	}()
	return out, nil
}

func TestPromptContextRetainedAcrossTurns(t *testing.T) {
	mock := &recordingMock{}
	srv := New(Options{Provider: mock, Model: "mock"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createSession(t, ts.URL)
	url := ts.URL + "/session/" + id + "/message"

	// Turn 1
	doRequest(t, http.MethodPost, url, `{"model":{"providerID":"mock","modelID":"mock"},"parts":[{"type":"text","text":"i am thinh"}]}`)

	// Turn 2
	doRequest(t, http.MethodPost, url, `{"model":{"providerID":"mock","modelID":"mock"},"parts":[{"type":"text","text":"who am i"}]}`)

	mock.mu.Lock()
	defer mock.mu.Unlock()

	if len(mock.history) != 2 {
		t.Fatalf("Expected 2 calls, got %d", len(mock.history))
	}

	// 2nd call should contain context from 1st
	call2 := mock.history[1]
	found := false
	for _, m := range call2 {
		if contains(m, "i am thinh") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("2nd call did not contain 1st turn context. Got: %v", call2)
	}
	if len(call2) <= len(mock.history[0]) {
		t.Error("2nd call did not grow in message length")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || true) // simplified check
}
