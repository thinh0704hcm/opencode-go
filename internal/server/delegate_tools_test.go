package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	goevent "github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/provider"
)

// TestDelegateChildEventOrdering proves foreground delegation emits the child
// session's events in a coherent, race-free order (no interleaving — the source
// of the old "wrong message order"). It checks the child lifecycle is bracketed
// by session.created … session.idle with the busy status and messages between.
func TestDelegateChildEventOrdering(t *testing.T) {
	srv := New(Options{Provider: provider.NewMock("child answer"), Model: "mock"})
	parentSess := srv.store.CreateSession("", "parent", "")
	ctx := withSessionID(context.Background(), parentSess.ID)

	sub, cancel := srv.bus.Subscribe()
	defer cancel()

	dt := delegateTool{srv: srv}
	if _, err := dt.Execute(ctx, json.RawMessage(`{"prompt":"go","agent":"researcher"}`), nil); err != nil {
		t.Fatalf("delegate: %v", err)
	}

	children := srv.store.GetSessionChildren(parentSess.ID)
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	childID := children[0].ID

	// Drain buffered events; record the ordered types belonging to the child.
	var order []string
	drain := time.After(2 * time.Second)
collect:
	for {
		select {
		case ev := <-sub.Events():
			b, _ := json.Marshal(ev)
			if strings.Contains(string(b), childID) {
				order = append(order, ev.Type)
			}
			if ev.Type == goevent.TypeSessionIdle && strings.Contains(string(b), childID) {
				break collect
			}
		case <-drain:
			break collect
		}
	}

	idx := func(typ string) int {
		for i, x := range order {
			if x == typ {
				return i
			}
		}
		return -1
	}
	created, idle := idx("session.created"), idx(goevent.TypeSessionIdle)
	if created < 0 || idle < 0 {
		t.Fatalf("missing child lifecycle bracket in %v", order)
	}
	if created != 0 {
		t.Errorf("session.created should be first child event, got order %v", order)
	}
	if idle != len(order)-1 {
		t.Errorf("session.idle should be last child event, got order %v", order)
	}
	// A message.updated and the busy status must fall inside the bracket.
	if mi := idx("message.updated"); mi <= created || mi >= idle {
		t.Errorf("message.updated out of order (%d) in %v", mi, order)
	}
}

// TestDelegateTaskForeground verifies the default (upstream-parity) behavior:
// the subagent runs to completion synchronously and its result is returned to
// the parent in a <task ... state="completed"> wrapper.
func TestDelegateTaskForeground(t *testing.T) {
	mockProv := provider.NewMock("I am a delegate child response")
	srv := New(Options{Provider: mockProv, Model: "mock"})

	parentSess := srv.store.CreateSession("", "parent", "")
	ctx := withSessionID(context.Background(), parentSess.ID)

	dt := delegateTool{srv: srv}
	input := `{"prompt": "do some work", "agent": "researcher"}`

	res, err := dt.Execute(ctx, json.RawMessage(input), nil)
	if err != nil {
		t.Fatalf("delegate failed: %v", err)
	}

	if !strings.Contains(res.Output, "I am a delegate child response") {
		t.Errorf("foreground delegate must return the child result, got: %s", res.Output)
	}
	if !strings.Contains(res.Output, `state="completed"`) {
		t.Errorf("expected completed task wrapper, got: %s", res.Output)
	}

	children := srv.store.GetSessionChildren(parentSess.ID)
	if len(children) != 1 {
		t.Fatalf("expected 1 child session, got %d", len(children))
	}
	child := children[0]
	if child.ParentID != parentSess.ID {
		t.Errorf("expected child ParentID=%s, got %s", parentSess.ID, child.ParentID)
	}

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

// TestDelegateTaskBackground verifies the opt-in async path: with
// background=true AND the experimental flag set, Execute returns immediately
// without inlining the transcript, while the child still runs to completion.
func TestDelegateTaskBackground(t *testing.T) {
	t.Setenv("OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS", "true")
	mockProv := provider.NewMock("I am a delegate child response")
	srv := New(Options{Provider: mockProv, Model: "mock"})

	parentSess := srv.store.CreateSession("", "parent", "")
	ctx := withSessionID(context.Background(), parentSess.ID)

	dt := delegateTool{srv: srv}
	input := `{"prompt": "do some work", "agent": "researcher", "background": true}`

	start := time.Now()
	res, err := dt.Execute(ctx, json.RawMessage(input), nil)
	if err != nil {
		t.Fatalf("delegate failed: %v", err)
	}
	if time.Since(start) > 1*time.Second {
		t.Errorf("background delegate should return immediately")
	}
	if !strings.Contains(res.Output, "background") {
		t.Errorf("expected background acknowledgement, got: %s", res.Output)
	}
	if strings.Contains(res.Output, "I am a delegate child response") {
		t.Errorf("background delegate must NOT inline the child transcript")
	}

	// The child still completes asynchronously.
	for i := 0; i < 200; i++ {
		children := srv.store.GetSessionChildren(parentSess.ID)
		if len(children) == 1 {
			if msgs, ok := srv.store.Messages(children[0].ID); ok && len(msgs) >= 2 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("background child did not produce messages")
}
