//go:build opencode_wip

package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/opencode-go/opencode-go/internal/provider"
)

func TestDelegateAbortPropagation(t *testing.T) {
	mockProv := provider.NewMock("")
	srv := New(Options{Provider: mockProv, Model: "mock"})

	// Create parent session
	parentSess := srv.store.CreateSession("", "parent", "")
	srv.store.AppendUserMessage(parentSess.ID, "", "mock", "mock", "build", []string{"run delegator"})
	_, _ = srv.store.NewAssistantMessage(parentSess.ID, "", "mock", "mock", "build", "build", false)
	ctx := withSessionID(context.Background(), parentSess.ID)

	dt := delegateTool{srv: srv}
	input := `{"prompt": "do some work", "agent": "researcher"}`

	// Delegated tasks are now non-blocking.
	res, err := dt.Execute(ctx, json.RawMessage(input), nil)
	if err != nil {
		t.Fatalf("delegate failed: %v", err)
	}

	var out struct {
		SessionID string `json:"sessionID"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}
	childSessID := out.SessionID

	// Wait for child to be running
	success := false
	for i := 0; i < 500; i++ {
		srv.sesMu.Lock()
		w, ok := srv.sesQueue[childSessID]
		running := false
		if ok && w != nil {
			running = w.running
		}
		srv.sesMu.Unlock()
		if running {
			success = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !success {
		t.Fatal("timed out waiting for child to start")
	}

	// Cancel child
	srv.cancelSession(childSessID)

	// Verify child idle/stopped
	for i := 0; i < 500; i++ {
		srv.sesMu.Lock()
		w, ok := srv.sesQueue[childSessID]
		running := true
		if ok && w != nil {
			running = w.running
		}
		srv.sesMu.Unlock()
		if !running {
			return // Success
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("child session failed to stop after cancel")
}
