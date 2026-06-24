package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/opencode-go/opencode-go/internal/event"
)

// TestTUIExecuteCommandPublishes verifies /tui/execute-command no longer 501s:
// it publishes a tui.command.execute event and returns true.
func TestTUIExecuteCommandPublishes(t *testing.T) {
	srv := New(Options{Provider: mockProvider{}})
	sub, cancel := srv.bus.Subscribe()
	defer cancel()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/tui/execute-command", "application/json", bytes.NewReader([]byte(`{"command":"/compact"}`)))
	if err != nil {
		t.Fatalf("execute-command: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var result any
	json.NewDecoder(resp.Body).Decode(&result)
	if b, ok := result.(bool); !ok || !b {
		t.Fatalf("want true, got %+v", result)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub.Events():
			if ev.Type == "tui.command.execute" {
				return
			}
		case <-deadline:
			t.Fatal("did not receive tui.command.execute event")
		}
	}
}

// TestSummarizeTriggersCompaction verifies the TUI's /compact path
// (POST /session/{id}/summarize) performs real compaction: it creates a
// compression block and emits the session.next.compaction lifecycle.
func TestSummarizeTriggersCompaction(t *testing.T) {
	srv := New(Options{Provider: mockProvider{}})
	sess := srv.store.CreateSession("", "test", "")
	// >8 messages so there is something to compress past the default keep-recent.
	for i := 0; i < 12; i++ {
		srv.store.AppendUserMessage(sess.ID, "", "", "", "", []string{"message body"})
	}

	sub, cancel := srv.bus.Subscribe()
	defer cancel()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/session/"+sess.ID+"/summarize", "application/json", bytes.NewReader([]byte(`{"providerID":"mock","modelID":"mock"}`)))
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if blocks := srv.store.CompressionBlocks(sess.ID); len(blocks) == 0 {
		t.Fatal("expected a compression block after summarize, got none")
	}

	gotStart, gotEnd := false, false
	deadline := time.After(2 * time.Second)
	for !(gotStart && gotEnd) {
		select {
		case ev := <-sub.Events():
			switch ev.Type {
			case event.TypeSessionNextCompactionStarted:
				gotStart = true
			case event.TypeSessionNextCompactionEnded:
				gotEnd = true
			}
		case <-deadline:
			t.Fatalf("missing compaction lifecycle: started=%v ended=%v", gotStart, gotEnd)
		}
	}
}
