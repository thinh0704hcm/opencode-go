package server

import (
    "context"
    "encoding/json"
    "strings"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"
    "github.com/opencode-go/opencode-go/internal/event"
    "github.com/opencode-go/opencode-go/internal/provider"
)

// mockProvider satisfies provider.Provider for testing compact.
type mockProvider struct{}

func (m mockProvider) ID() string { return "mock" }
func (m mockProvider) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatChunk, error) {
    ch := make(chan provider.ChatChunk)
    go func() {
        defer close(ch)
        ch <- provider.ChatChunk{TextDelta: "summary"}
    }()
    return ch, nil
}

func TestDCPContextReturnsSessionData(t *testing.T) {
    srv := New(Options{Provider: mockProvider{}})
    // create session
    sess := srv.store.CreateSession("", "test", "")
    // add a user message
    _, ok := srv.store.AppendUserMessage(sess.ID, "", "", "", "", []string{"hello"})
    if !ok {
        t.Fatalf("append user message failed")
    }
    // request context endpoint
    req := httptest.NewRequest("GET", "/api/session/"+sess.ID+"/dcp/context", nil)
    rec := httptest.NewRecorder()
    srv.Handler().ServeHTTP(rec, req)
    if rec.Code != http.StatusOK {
        t.Fatalf("unexpected status %d", rec.Code)
    }
    var resp struct{ Data map[string]any `json:"data"` }
    if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
        t.Fatalf("unmarshal response: %v", err)
    }
    d := resp.Data
    if _, ok := d["messageCount"].(float64); !ok { // json numbers decode as float64
        t.Fatalf("messageCount missing or not a number")
    }
    if d["session"] == nil {
        t.Fatalf("session missing")
    }
}

func TestCompactEmitsBalancedEvents(t *testing.T) {
    srv := New(Options{Provider: mockProvider{}})
    sess := srv.store.CreateSession("", "test", "")
    // add messages to have something to compress
    srv.store.AppendUserMessage(sess.ID, "", "", "", "", []string{"msg1"})
    srv.store.AppendUserMessage(sess.ID, "", "", "", "", []string{"msg2"})
    sub, cancel := srv.bus.Subscribe()
    defer cancel()
    // compact
    _, _, err := srv.compactSession(sess.ID, compactRequest{Mode: "manual", Focus: "", KeepRecent: 0})
    if err != nil {
        t.Fatalf("compact error: %v", err)
    }
    // collect events
    gotStart, gotEnd := false, false
    timeout := time.After(2 * time.Second)
    for !(gotStart && gotEnd) {
        select {
        case ev := <-sub.Events():
            if ev.Type == event.TypeCompactionStarted {
                gotStart = true
            }
            if ev.Type == event.TypeCompactionEnded {
                gotEnd = true
            }
        case <-timeout:
            t.Fatalf("timeout waiting for compaction events: start=%v end=%v", gotStart, gotEnd)
        }
    }
    if !gotStart || !gotEnd {
        t.Fatalf("did not receive both compaction events")
    }
}

// TestCompactEmitsCanonicalNextEvents verifies the upstream-parity
// session.next.compaction.{started,delta,ended} lifecycle: a shared messageID,
// the reason carried through, a streamed delta, and a balanced start/end.
func TestCompactEmitsCanonicalNextEvents(t *testing.T) {
    srv := New(Options{Provider: mockProvider{}})
    sess := srv.store.CreateSession("", "test", "")
    srv.store.AppendUserMessage(sess.ID, "", "", "", "", []string{"msg1"})
    srv.store.AppendUserMessage(sess.ID, "", "", "", "", []string{"msg2"})
    srv.store.AppendUserMessage(sess.ID, "", "", "", "", []string{"msg3"})

    sub, cancel := srv.bus.Subscribe()
    defer cancel()

    if _, _, err := srv.compactSession(sess.ID, compactRequest{Reason: "manual", KeepRecent: 1}); err != nil {
        t.Fatalf("compact error: %v", err)
    }

    var startID, deltaID, endID, endReason, endText string
    var gotStart, gotDelta, gotEnd bool
    timeout := time.After(2 * time.Second)
    for !(gotStart && gotDelta && gotEnd) {
        select {
        case ev := <-sub.Events():
            switch ev.Type {
            case event.TypeSessionNextCompactionStarted:
                p := ev.Properties.(event.SessionNextCompactionStartedProps)
                startID, gotStart = p.MessageID, true
                if p.Reason != "manual" {
                    t.Errorf("started reason = %q, want manual", p.Reason)
                }
            case event.TypeSessionNextCompactionDelta:
                p := ev.Properties.(event.SessionNextCompactionDeltaProps)
                deltaID, gotDelta = p.MessageID, true
                if p.Text == "" {
                    t.Error("delta text empty")
                }
            case event.TypeSessionNextCompactionEnded:
                p := ev.Properties.(event.SessionNextCompactionEndedProps)
                endID, endReason, endText, gotEnd = p.MessageID, p.Reason, p.Text, true
            }
        case <-timeout:
            t.Fatalf("timeout: start=%v delta=%v end=%v", gotStart, gotDelta, gotEnd)
        }
    }

    if startID == "" || startID != deltaID || startID != endID {
        t.Errorf("messageID not shared across lifecycle: start=%q delta=%q end=%q", startID, deltaID, endID)
    }
    if endReason != "manual" {
        t.Errorf("ended reason = %q, want manual", endReason)
    }
    if !strings.Contains(endText, "summary") {
        t.Errorf("ended text missing summary, got %q", endText)
    }
}
