package session

import "testing"

func TestAppendToolPartCompletedNewPartHasNonZeroDuration(t *testing.T) {
	s := NewStore()
	sess := s.CreateSession("", "Test", "/work")
	am, ok := s.NewAssistantMessage(sess.ID, "", "concactao", "model", "build", "build")
	if !ok {
		t.Fatal("new assistant msg failed")
	}

	part, ok := s.AppendToolPart(sess.ID, am.Info.ID, "read", "", "completed", map[string]any{"path": "README.md"}, "ok")
	if !ok {
		t.Fatal("append tool part failed")
	}
	if part.State == nil || part.State.Time == nil {
		t.Fatalf("missing tool state time: %+v", part.State)
	}
	if part.State.Time.End == nil || *part.State.Time.End <= part.State.Time.Start {
		t.Fatalf("want End > Start, got start=%d end=%v", part.State.Time.Start, part.State.Time.End)
	}
	if got := *part.State.Time.End - part.State.Time.Start; got < 1 {
		t.Fatalf("want duration >= 1ms, got %d", got)
	}
}

func TestAppendToolPartNewPartPreservesTitleMetadata(t *testing.T) {
	s := NewStore()
	sess := s.CreateSession("", "Test", "/work")
	am, ok := s.NewAssistantMessage(sess.ID, "", "concactao", "model", "build", "build")
	if !ok {
		t.Fatal("new assistant msg failed")
	}
	input := map[string]any{"agent": "tester", "description": "check metadata"}
	output := "done"

	part, ok := s.AppendToolPart(sess.ID, am.Info.ID, "delegate", "", "running", input, output)
	if !ok {
		t.Fatal("append tool part failed")
	}
	wantTitle, wantMetadata := toolDisplay("delegate", input, output)
	if part.State == nil {
		t.Fatal("missing tool state")
	}
	if part.State.Title != wantTitle {
		t.Fatalf("title mismatch: want %q got %q", wantTitle, part.State.Title)
	}
	if part.State.Metadata["output"] != wantMetadata["output"] || part.State.Metadata["description"] != wantMetadata["description"] {
		t.Fatalf("metadata mismatch: want %+v got %+v", wantMetadata, part.State.Metadata)
	}
}
