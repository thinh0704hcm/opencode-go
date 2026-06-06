package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// sseServer stands up an httptest.Server that writes the given SSE data lines
// (each emitted as "data: <line>\n\n" and flushed) followed by "data: [DONE]".
func sseServer(t *testing.T, lines []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("ResponseWriter does not support Flush")
		}
		for _, ln := range lines {
			if _, err := w.Write([]byte("data: " + ln + "\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
}

// collectToolCall drains the channel and returns the first chunk with ToolCall != nil.
func collectToolCall(t *testing.T, ch <-chan ChatChunk) *ToolCall {
	t.Helper()
	var found *ToolCall
	for cc := range ch {
		if cc.Err != nil {
			t.Fatalf("unexpected error chunk: %v", cc.Err)
		}
		if cc.ToolCall != nil && found == nil {
			found = cc.ToolCall
		}
	}
	return found
}

func runToolCallStream(t *testing.T, lines []string) *ToolCall {
	t.Helper()
	srv := sseServer(t, lines)
	defer srv.Close()

	p := NewOpenAI("test", srv.URL, "", "test-model", srv.Client())
	ch, err := p.StreamChat(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		Tools:    []ToolSchema{{Name: "bash"}},
	})
	if err != nil {
		t.Fatalf("StreamChat returned error: %v", err)
	}
	return collectToolCall(t, ch)
}

func assertBashEchoHi(t *testing.T, tc *ToolCall) {
	t.Helper()
	if tc == nil {
		t.Fatalf("no ToolCall chunk emitted")
	}
	if tc.Name != "bash" {
		t.Errorf("ToolCall.Name = %q, want %q", tc.Name, "bash")
	}
	if tc.ID != "call_1" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call_1")
	}
	const wantInput = `{"command":"echo hi"}`
	if string(tc.Input) != wantInput {
		t.Errorf("ToolCall.Input = %q, want %q", string(tc.Input), wantInput)
	}
	var parsed struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(tc.Input, &parsed); err != nil {
		t.Fatalf("json.Unmarshal(Input) failed: %v (Input=%q)", err, string(tc.Input))
	}
	if parsed.Command != "echo hi" {
		t.Errorf("parsed Command = %q, want %q", parsed.Command, "echo hi")
	}
}

// TestOpenAIToolCallAccumulation: arguments fragmented across multiple deltas,
// finish_reason "tool_calls" in a separate chunk.
func TestOpenAIToolCallAccumulation(t *testing.T) {
	lines := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":""}}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"comm"}}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"and\":\"echo hi\"}"}}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	assertBashEchoHi(t, runToolCallStream(t, lines))
}

// TestOpenAIToolCallSingleChunk: whole tool call (id+name+full args) plus
// finish_reason "tool_calls" all arrive in ONE delta.
func TestOpenAIToolCallSingleChunk(t *testing.T) {
	lines := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo hi\"}"}}]},"finish_reason":"tool_calls"}]}`,
	}
	assertBashEchoHi(t, runToolCallStream(t, lines))
}

// TestOpenAIToolCallFinishStop: gateway sends tool-call fragments but
// finish_reason "stop" (not "tool_calls"); the tool call must still be emitted
// via the [DONE] flush path.
func TestOpenAIToolCallFinishStop(t *testing.T) {
	lines := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":""}}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"comm"}}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"and\":\"echo hi\"}"}}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}
	assertBashEchoHi(t, runToolCallStream(t, lines))
}
