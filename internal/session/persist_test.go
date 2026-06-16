package session

import (
	"testing"
)

// TestLoadClosesZombieAssistantMessages verifies that assistant messages with
// time.completed==nil (server killed mid-turn) are closed on Load so the TUI
// does not lock input waiting for a generation that will never complete.
func TestLoadClosesZombieAssistantMessages(t *testing.T) {
	dir := t.TempDir()
	s := NewStore()
	if err := s.SetPersistDir(dir); err != nil {
		t.Fatal(err)
	}
	sess := s.CreateSession("", "", "")
	if _, ok := s.AppendUserMessage(sess.ID, "", "", "", "build", []string{"hello"}); !ok {
		t.Fatal("AppendUserMessage failed")
	}
	am, ok := s.NewAssistantMessage(sess.ID, "", "", "", "build", "build")
	if !ok {
		t.Fatal("NewAssistantMessage failed")
	}
	// Verify the message is NOT completed yet.
	if am.Info.Time.Completed != nil {
		t.Fatal("new assistant message should not be completed")
	}
	// Persist WITHOUT completing the assistant message (simulates server kill).
	s.PersistSession(sess.ID)

	// Load into a fresh store — zombie must be closed.
	s2 := NewStore()
	if err := s2.SetPersistDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	msgs, ok := s2.Messages(sess.ID)
	if !ok {
		t.Fatal("session not loaded")
	}
	for _, m := range msgs {
		if m.Info.Role == "assistant" {
			if m.Info.Time.Completed == nil {
				t.Fatal("zombie assistant message was not closed on Load")
			}
			if m.Info.Finish != "aborted" {
				t.Fatalf("expected finish=aborted, got %q", m.Info.Finish)
			}
		}
	}
}

func TestSessionPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewStore()
	if err := s.SetPersistDir(dir); err != nil {
		t.Fatal(err)
	}
	sess := s.CreateSession("", "My Title", "/work")
	if _, ok := s.AppendUserMessage(sess.ID, "", "concactao", "cx/gpt-5.5-review", "build", []string{"hello"}); !ok {
		t.Fatal("append user msg failed")
	}
	am, ok := s.NewAssistantMessage(sess.ID, "", "concactao", "cx/gpt-5.5-review", "build", "build")
	if !ok {
		t.Fatal("new assistant msg failed")
	}
	// Tool part with numeric Input arg (locks in map[string]any round-trip).
	if _, ok := s.AppendToolPart(sess.ID, am.Info.ID, "read", "call-1", "completed",
		map[string]any{"path": "MEMORY.md", "limit": 50}, "file contents"); !ok {
		t.Fatal("append tool part failed")
	}
	s.PersistSession(sess.ID)

	// Fresh store loads from the same dir.
	s2 := NewStore()
	if err := s2.SetPersistDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	got, ok := s2.GetSession(sess.ID)
	if !ok {
		t.Fatalf("session %s not loaded", sess.ID)
	}
	if got.Title != "My Title" || got.Directory != "/work" {
		t.Fatalf("session fields lost: %+v", got)
	}
	msgs, ok := s2.Messages(sess.ID)
	if !ok || len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d ok=%v", len(msgs), ok)
	}
	// Find the tool part and assert State + numeric Input survived.
	var found bool
	for _, m := range msgs {
		for _, p := range m.Parts {
			if p.Type == "tool" && p.CallID == "call-1" {
				found = true
				if p.State == nil || p.State.Status != "completed" {
					t.Fatalf("tool state lost: %+v", p.State)
				}
				if p.State.Input["path"] != "MEMORY.md" {
					t.Fatalf("tool input path lost: %+v", p.State.Input)
				}
				// JSON numbers decode as float64.
				if v, ok := p.State.Input["limit"].(float64); !ok || v != 50 {
					t.Fatalf("numeric input arg lost/drifted: %v (%T)", p.State.Input["limit"], p.State.Input["limit"])
				}
			}
		}
	}
	if !found {
		t.Fatal("tool part not found after reload")
	}
}
