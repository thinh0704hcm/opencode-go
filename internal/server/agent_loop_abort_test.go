package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/session"
	"github.com/opencode-go/opencode-go/internal/tool"
)

type abortTestProvider struct {
	calls []provider.ToolCall
	turns int
}

func (p *abortTestProvider) ID() string { return "abort-test" }

func (p *abortTestProvider) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatChunk, error) {
	p.turns++
	out := make(chan provider.ChatChunk, len(p.calls)+1)
	go func() {
		defer close(out)
		if p.turns == 1 {
			for i := range p.calls {
				c := p.calls[i]
				select {
				case out <- provider.ChatChunk{ToolCall: &c}:
				case <-ctx.Done():
					return
				}

			}
			select {
			case out <- provider.ChatChunk{FinishReason: "tool_calls"}:
			case <-ctx.Done():
			}
			return
		}
		select {
			case out <- provider.ChatChunk{TextDelta: "final"}:
			case <-ctx.Done():
				return
			}
		select {
			case out <- provider.ChatChunk{FinishReason: "stop"}:
			case <-ctx.Done():
			}
	}()
	return out, nil
}

type abortTestTool struct {
	name    string
	out     string
	done    chan<- string
	waitCtx bool // block Execute until ctx is cancelled (for abort-timing tests)
}

func (t abortTestTool) Name() string   { return t.name }
func (t abortTestTool) Mutating() bool { return false }
func (t abortTestTool) Execute(ctx context.Context, input json.RawMessage, sb *tool.Sandbox) (tool.Result, error) {
	if t.done != nil {
		select {
			case t.done <- t.name:
			default:
		}
	}
	if t.waitCtx {
		<-ctx.Done()
	}
	return tool.Result{Output: t.out}, nil
}

func newAbortLoopServer(t *testing.T, calls []provider.ToolCall, done chan<- string, firstWaitsCtx bool) (*Server, *abortTestProvider) {
	t.Helper()
	p := &abortTestProvider{calls: calls}
	r := tool.NewRegistry()
	r.Register(abortTestTool{name: "first", out: "first ok", done: done, waitCtx: firstWaitsCtx})
	r.Register(abortTestTool{name: "second", out: "second ok", done: done})
	srv := New(Options{Provider: p, Model: "abort-test", Tools: r, Workdir: t.TempDir(), DataDir: t.TempDir()})
	srv.store = session.NewStore()
	return srv, p
}

func newAbortLoopMessage(t *testing.T, srv *Server) (string, string, string) {
	t.Helper()
	sess := srv.store.CreateSession("", "test", "")
	u, ok := srv.store.AppendUserMessage(sess.ID, "u", "abort-test", "abort-test", "agent", []string{"hi"})
	if !ok {
		t.Fatal("AppendUserMessage failed")
	}
	asst, ok := srv.store.NewAssistantMessage(sess.ID, u.Info.ID, "abort-test", "abort-test", "agent", "mode", false)
	if !ok {
		t.Fatal("NewAssistantMessage failed")
	}
	return sess.ID, u.Info.ID, asst.Info.ID
}

func collectAbortEvents(sub *event.Subscriber) []event.Event {
	var events []event.Event
	for {
		select {
		case ev := <-sub.Events():
			events = append(events, ev)
		default:
			return events
		}
	}
}

func toolPartByCallID(t *testing.T, srv *Server, sessionID, callID string) session.Part {
	t.Helper()
	msgs, ok := srv.store.Messages(sessionID)
	if !ok {
		t.Fatal("Messages failed")
	}
	for _, msg := range msgs {
		for _, part := range msg.Parts {
			if part.Type == "tool" && part.CallID == callID {
				return part
			}
		}
	}
	t.Fatalf("missing tool part %s", callID)
	return session.Part{}
}

func countEvents(events []event.Event, typ string) int {
	n := 0
	for _, ev := range events {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

func TestAgentLoopAbortBeforeFirstTurn(t *testing.T) {
	srv, p := newAbortLoopServer(t, nil, nil, false)
	sub, cancelSub := srv.bus.Subscribe()
	defer cancelSub()
	sessionID, userID, messageID := newAbortLoopMessage(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := srv.runAgentLoop(ctx, sessionID, messageID, userID, "abort-test/abort-test", []string{"hi"}, nil, "", Agent{Name: "agent"})
	if got != "aborted" {
		t.Fatalf("runAgentLoop = %q, want aborted", got)
	}
	if p.turns != 0 {
		t.Fatalf("provider turns = %d, want 0", p.turns)
	}
	if countEvents(collectAbortEvents(sub), "session.next.step.started") != 0 {
		t.Fatal("unexpected step-start event after pre-turn cancellation")
	}
}

func TestAgentLoopAbortBetweenToolCalls(t *testing.T) {
	done := make(chan string, 2)
	calls := []provider.ToolCall{{ID: "c1", Name: "first", Input: json.RawMessage(`{}`)},{ID: "c2", Name: "second", Input: json.RawMessage(`{}`)}}
	srv, _ := newAbortLoopServer(t, calls, done, true)
	sub, cancelSub := srv.bus.Subscribe()
	defer cancelSub()
	sessionID, userID, messageID := newAbortLoopMessage(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-done; cancel() }()

	got := srv.runAgentLoop(ctx, sessionID, messageID, userID, "abort-test/abort-test", []string{"hi"}, nil, "", Agent{Name: "agent"})
	if got != "aborted" {
		t.Fatalf("runAgentLoop = %q, want aborted", got)
	}
	first := toolPartByCallID(t, srv, sessionID, "c1")
	if first.State == nil || first.State.Status != "completed" || first.State.Output != "first ok" {
		t.Fatalf("first tool state = %#v, want completed first ok", first.State)
	}
	second := toolPartByCallID(t, srv, sessionID, "c2")
	if second.State == nil || second.State.Status != "error" || second.State.Error != "Tool execution aborted" {
		t.Fatalf("second tool state = %#v, want aborted error", second.State)
	}
	if countEvents(collectAbortEvents(sub), "session.next.tool.failed") != 1 {
		t.Fatal("missing session.next.tool.failed for aborted pending tool")
	}
}

func TestAgentLoopAbortAfterToolBatch(t *testing.T) {
	done := make(chan string, 1)
	calls := []provider.ToolCall{{ID: "c1", Name: "first", Input: json.RawMessage(`{}`)}}
	srv, p := newAbortLoopServer(t, calls, done, true)
	sessionID, userID, messageID := newAbortLoopMessage(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-done; cancel() }()

	got := srv.runAgentLoop(ctx, sessionID, messageID, userID, "abort-test/abort-test", []string{"hi"}, nil, "", Agent{Name: "agent"})
	if got != "aborted" {
		t.Fatalf("runAgentLoop = %q, want aborted", got)
	}
	if p.turns != 1 {
		t.Fatalf("provider turns = %d, want 1", p.turns)
	}
	first := toolPartByCallID(t, srv, sessionID, "c1")
	if first.State == nil || first.State.Status != "completed" || first.State.Output != "first ok" {
		t.Fatalf("first tool state = %#v, want completed first ok", first.State)
	}
}

// --- Doom-loop detection tests ---

func addToolParts(t *testing.T, srv *Server, sessionID, messageID string, toolName string, inputs []map[string]any, statuses []string) {
	t.Helper()
	for i, input := range inputs {
		status := "completed"
		if i < len(statuses) {
			status = statuses[i]
		}
		callID := fmt.Sprintf("doom_%s_%d", toolName, i)
		p, ok := srv.store.AppendToolPart(sessionID, messageID, toolName, callID, status, input, "output")
		if !ok {
			t.Fatalf("AppendToolPart #%d failed", i)
		}
		_ = p
	}
}

func TestDetectDoomLoop_FewerThanThreshold(t *testing.T) {
	srv, _ := newAbortLoopServer(t, nil, nil, false)
	sessionID, _, messageID := newAbortLoopMessage(t, srv)

	// Only 2 tool parts — below threshold of 3
	inputs := []map[string]any{{"cmd":"ls"}, {"cmd":"ls"}}
	addToolParts(t, srv, sessionID, messageID, "bash", inputs, nil)

	if srv.detectDoomLoop(sessionID, messageID, "bash", json.RawMessage(`{"cmd":"ls"}`)) {
		t.Fatal("should not detect doom loop with < 3 parts")
	}
}

func TestDetectDoomLoop_ThreeIdenticalCompleted(t *testing.T) {
	srv, _ := newAbortLoopServer(t, nil, nil, false)
	sessionID, _, messageID := newAbortLoopMessage(t, srv)

	inputs := []map[string]any{{"cmd":"ls"}, {"cmd":"ls"}, {"cmd":"ls"}}
	addToolParts(t, srv, sessionID, messageID, "bash", inputs, nil)

	if !srv.detectDoomLoop(sessionID, messageID, "bash", json.RawMessage(`{"cmd":"ls"}`)) {
		t.Fatal("should detect doom loop with 3 identical completed parts")
	}
}

func TestDetectDoomLoop_DifferentToolNames(t *testing.T) {
	srv, _ := newAbortLoopServer(t, nil, nil, false)
	sessionID, _, messageID := newAbortLoopMessage(t, srv)

	// Mix tool names
inputs := []map[string]any{{"cmd":"ls"}, {"cmd":"ls"}, {"cmd":"ls"}}
statuses := []string{"completed", "completed", "completed"}
addToolParts(t, srv, sessionID, messageID, "bash", inputs[:1], statuses[:1])
addToolParts(t, srv, sessionID, messageID, "bash", inputs[1:2], statuses[1:2])
p, ok := srv.store.AppendToolPart(sessionID, messageID, "other_tool", "doom_other_0", "completed", inputs[2], "output")
	if !ok {
		t.Fatal("AppendToolPart failed")
	}
	_ = p

	if srv.detectDoomLoop(sessionID, messageID, "bash", json.RawMessage(`{"cmd":"ls"}`)) {
		t.Fatal("should not detect doom loop when last part has different tool name")
	}
}

func TestDetectDoomLoop_DifferentInputs(t *testing.T) {
	srv, _ := newAbortLoopServer(t, nil, nil, false)
	sessionID, _, messageID := newAbortLoopMessage(t, srv)

inputs := []map[string]any{{"cmd":"ls"}, {"cmd":"pwd"}, {"cmd":"ls"}}
addToolParts(t, srv, sessionID, messageID, "bash", inputs, nil)

	if srv.detectDoomLoop(sessionID, messageID, "bash", json.RawMessage(`{"cmd":"ls"}`)) {
		t.Fatal("should not detect doom loop when inputs differ")
	}
}

func TestDetectDoomLoop_PendingStatus(t *testing.T) {
	srv, _ := newAbortLoopServer(t, nil, nil, false)
	sessionID, _, messageID := newAbortLoopMessage(t, srv)

inputs := []map[string]any{{"cmd":"ls"}, {"cmd":"ls"}, {"cmd":"ls"}}
statuses := []string{"completed", "completed", "pending"}
addToolParts(t, srv, sessionID, messageID, "bash", inputs, statuses)

	if srv.detectDoomLoop(sessionID, messageID, "bash", json.RawMessage(`{"cmd":"ls"}`)) {
		t.Fatal("should not detect doom loop when last part is pending")
	}
}

func TestDetectDoomLoop_RunningStatus(t *testing.T) {
	srv, _ := newAbortLoopServer(t, nil, nil, false)
	sessionID, _, messageID := newAbortLoopMessage(t, srv)

inputs := []map[string]any{{"cmd":"ls"}, {"cmd":"ls"}, {"cmd":"ls"}}
statuses := []string{"completed", "completed", "running"}
addToolParts(t, srv, sessionID, messageID, "bash", inputs, statuses)

	if srv.detectDoomLoop(sessionID, messageID, "bash", json.RawMessage(`{"cmd":"ls"}`)) {
		t.Fatal("should not detect doom loop when last part is running")
	}
}

func TestDetectDoomLoop_MixedTextAndToolParts(t *testing.T) {
	srv, _ := newAbortLoopServer(t, nil, nil, false)
	sessionID, _, messageID := newAbortLoopMessage(t, srv)

	// Add text part first, then 3 identical tool parts
	srv.store.AppendTextDelta(sessionID, messageID, "text", "thinking...")
	inputs := []map[string]any{{"cmd":"ls"}, {"cmd":"ls"}, {"cmd":"ls"}}
	addToolParts(t, srv, sessionID, messageID, "bash", inputs, nil)

	if !srv.detectDoomLoop(sessionID, messageID, "bash", json.RawMessage(`{"cmd":"ls"}`)) {
		t.Fatal("should detect doom loop with text part before identical tool parts")
	}
}

func TestDetectDoomLoop_JSONKeyOrdering(t *testing.T) {
	srv, _ := newAbortLoopServer(t, nil, nil, false)
	sessionID, _, messageID := newAbortLoopMessage(t, srv)

	// Store with one key ordering, query with different ordering
	inputs := []map[string]any{{"b":2,"a":1}, {"b":2,"a":1}, {"b":2,"a":1}}
	addToolParts(t, srv, sessionID, messageID, "bash", inputs, nil)

	if !srv.detectDoomLoop(sessionID, messageID, "bash", json.RawMessage(`{"a":1,"b":2}`)) {
		t.Fatal("should detect doom loop regardless of JSON key ordering")
	}
}
