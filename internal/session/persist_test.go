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

func TestNextSeqSurvivesRestart(t *testing.T) {
	dir := t.TempDir()

	// Session 1: create messages
	s1 := NewStore()
	if err := s1.SetPersistDir(dir); err != nil {
		t.Fatal(err)
	}
	sess := s1.CreateSession("", "Test", "/work")
	s1.AppendUserMessage(sess.ID, "", "", "", "build", []string{"hello"})
	s1.NewAssistantMessage(sess.ID, "", "", "", "build", "build")
	s1.AppendTextDelta(sess.ID, "", "text", "response text")
	s1.PersistSession(sess.ID)

	// Find max seq in session 1
	msgs1, _ := s1.Messages(sess.ID)
	var maxLoadedSeq uint64
	for _, m := range msgs1 {
		if m.Info.GlobalSeq > maxLoadedSeq {
			maxLoadedSeq = m.Info.GlobalSeq
		}
		for _, p := range m.Parts {
			if p.GlobalSeq > maxLoadedSeq {
				maxLoadedSeq = p.GlobalSeq
			}
		}
	}
	if maxLoadedSeq == 0 {
		t.Fatal("expected non-zero max seq in session 1")
	}

	// Load in fresh store (simulates restart)
	s2 := NewStore()
	if err := s2.SetPersistDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}

	// Create new message in loaded session
	s2.AppendUserMessage(sess.ID, "", "", "", "build", []string{"after restart"})
	msgs2, _ := s2.Messages(sess.ID)
	if len(msgs2) != len(msgs1)+1 {
		t.Fatalf("expected %d messages, got %d", len(msgs1)+1, len(msgs2))
	}

	// The new message's GlobalSeq must be > max of loaded seqs
	newMsg := msgs2[len(msgs2)-1]
	if newMsg.Info.GlobalSeq <= maxLoadedSeq {
		t.Errorf("new message GlobalSeq %d <= max loaded GlobalSeq %d", newMsg.Info.GlobalSeq, maxLoadedSeq)
	}
}

// TestPersistTodosSurviveRestart verifies todos survive store reload.
func TestPersistTodosSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	// Write with todos
	s1 := NewStore()
	if err := s1.SetPersistDir(dir); err != nil {
		t.Fatal(err)
	}
	sess := s1.CreateSession("ses_test", "", "")
	s1.SetTodos(sess.ID, []Todo{{Content: "First task", Status: "pending", Priority: ""}, {Content: "Second task", Status: "completed", Priority: ""}})
	// Persist (SetTodos already persisted)
	// Load in fresh store
	s2 := NewStore()
	if err := s2.SetPersistDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	todos, ok := s2.GetTodos(sess.ID)
	if !ok {
		t.Fatal("todos not loaded")
	}
	if len(todos) != 2 {
		t.Fatalf("expected 2 todos, got %d", len(todos))
	}
	if todos[0].Content != "First task" {
		t.Errorf("todo[0].Content = %q, want %q", todos[0].Content, "First task")
	}
	if todos[1].Status != "completed" {
		t.Errorf("todo[1].Status = %q, want %q", todos[1].Status, "completed")
	}
}
