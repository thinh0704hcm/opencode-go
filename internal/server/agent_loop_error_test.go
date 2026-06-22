//go:build opencode_wip

package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/tool"
)

// errorProvider emits a partial text chunk then an error.
type errorProvider struct{}

func (e *errorProvider) ID() string { return "err" }

func (e *errorProvider) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatChunk, error) {
	out := make(chan provider.ChatChunk)
	go func() {
		defer close(out)
		// partial content
		out <- provider.ChatChunk{TextDelta: "partial"}
		// error
		out <- provider.ChatChunk{Err: fmt.Errorf("stream error")}
	}()
	return out, nil
}

// helper to subscribe SSE events.
func subscribeEventsErr(t *testing.T, body *http.Response) <-chan sseFrame {
	t.Helper()
	events := make(chan sseFrame, 256)
	go func() {
		scanner := bufio.NewScanner(body.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
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

func TestAgentLoopStreamErrorDropsPartial(t *testing.T) {
	dir := t.TempDir()
	srv := New(Options{Provider: &errorProvider{}, Model: "err", Tools: tool.NewDefaultRegistry(), Workdir: dir})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// create session
	resp, err := http.Post(ts.URL+"/session", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	var sess struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&sess)
	resp.Body.Close()
	if sess.ID == "" {
		t.Fatalf("no session ID")
	}

	// subscribe events
	stream, err := http.Get(ts.URL + "/global/event?directory=/work")
	if err != nil {
		t.Fatalf("event stream: %v", err)
	}
	defer stream.Body.Close()
	events := subscribeEventsErr(t, stream)
	waitForType(t, events, event.TypeServerConnected, "/work")

	// send prompt_async
	body := `{"model":{"providerID":"err","modelID":"err"},"agent":"build","parts":[{"type":"text","text":"hi"}]}`
	preq, _ := http.NewRequest(http.MethodPost, ts.URL+"/session/"+sess.ID+"/prompt_async", strings.NewReader(body))
	preq.Header.Set("Content-Type", "application/json")
	presp, err := http.DefaultClient.Do(preq)
	if err != nil {
		t.Fatalf("prompt_async: %v", err)
	}
	presp.Body.Close()
	if presp.StatusCode != http.StatusNoContent {
		t.Fatalf("prompt_async status = %d, want 204", presp.StatusCode)
	}

	// collect events until error or timeout
	sawText := false
	sawErr := false
	deadline := time.After(5 * time.Second)
	for !sawErr {
		select {
		case ev := <-events:
			var pe struct {
				Type       string          `json:"type"`
				Properties json.RawMessage `json:"properties"`
			}
			json.Unmarshal(ev.Payload, &pe)
			if pe.Type == event.TypeMessagePartDelta {
				var p struct {
					SessionID string `json:"sessionID"`
					Field     string `json:"field"`
					Delta     string `json:"delta"`
				}
				json.Unmarshal(pe.Properties, &p)
				if p.Field == "text" {
					sawText = true
				}
			}
			if pe.Type == event.TypeSessionError {
				sawErr = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for session.error")
		}
	}
	if sawText {
		t.Error("saw partial text delta despite stream error; should have been dropped")
	}
	if !sawErr {
		t.Error("did not receive session.error event")
	}
}
