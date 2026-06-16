package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/opencode-go/opencode-go/internal/provider"
)

func TestDelegateTaskAsync(t *testing.T) {
	mockProv := provider.NewMock("I am a delegate child response")
	srv := New(Options{Provider: mockProv, Model: "mock"})

	// Create parent session
	parentSess := srv.store.CreateSession("", "parent", "")
	
	// Create context with parent session ID
	ctx := withSessionID(context.Background(), parentSess.ID)

	dt := delegateTool{srv: srv}
	
	input := `{"prompt": "do some work", "agent": "researcher"}`
	
	start := time.Now()
	res, err := dt.Execute(ctx, json.RawMessage(input), nil)
	if err != nil {
		t.Fatalf("delegate failed: %v", err)
	}
	duration := time.Since(start)
	
	if duration > 1*time.Second {
		t.Errorf("delegate execute blocked, took %v", duration)
	}
	
	if !strings.Contains(res.Output, "Delegated task") {
		t.Errorf("expected delegate output to have task ID, got: %s", res.Output)
	}
	
	if strings.Contains(res.Output, "I am a delegate child response") {
		t.Errorf("expected delegate output NOT to contain child transcript")
	}

	children := srv.store.GetSessionChildren(parentSess.ID)
	if len(children) != 1 {
		t.Fatalf("expected 1 child session, got %d", len(children))
	}
	
	child := children[0]
	if child.ParentID != parentSess.ID {
		t.Errorf("expected child ParentID=%s, got %s", parentSess.ID, child.ParentID)
	}
	
	// Wait a bit to let child goroutine finish
	time.Sleep(300 * time.Millisecond)
	
	messages, ok := srv.store.Messages(child.ID)
	if !ok {
		t.Fatal("expected messages in child session")
	}
	
	if len(messages) < 2 {
		t.Fatalf("expected at least 2 messages (user, assistant), got %d", len(messages))
	}
	
	if messages[0].Info.Role != "user" {
		t.Errorf("expected first message to be user, got %s", messages[0].Info.Role)
	}
	
	if messages[1].Info.Role != "assistant" {
		t.Errorf("expected second message to be assistant, got %s", messages[1].Info.Role)
	}
	
	if messages[1].Info.ParentID != messages[0].Info.ID {
		t.Errorf("expected assistant parentID %q to match user msg ID %q", messages[1].Info.ParentID, messages[0].Info.ID)
	}
}
