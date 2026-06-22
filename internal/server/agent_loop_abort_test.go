package server

import (
	"context"
	"encoding/json"
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
	calls := []provider.ToolCall{
		{ID: "c1", Name: "first", Input: json.RawMessage(`{}`)},
		{ID: "c2", Name: "second", Input: json.RawMessage(`{}`)},
	}
	srv, _ := newAbortLoopServer(t, calls, done, true)
	sub, cancelSub := srv.bus.Subscribe()
	defer cancelSub()
	sessionID, userID, messageID := newAbortLoopMessage(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-done
		cancel()
	}()

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
	go func() {
		<-done
		cancel()
	}()

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
