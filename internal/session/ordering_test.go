package session

import (
	"testing"
)

func TestAppendTextDeltaOrdering(t *testing.T) {
	s := NewStore()
	sess := s.CreateSession("", "Test", "/work")
	am, ok := s.NewAssistantMessage(sess.ID, "", "provider", "model", "build", "build")
	if !ok {
		t.Fatal("failed to create assistant message")
	}

	s.AppendTextDelta(sess.ID, am.Info.ID, "text", "before")
	s.AppendToolPart(sess.ID, am.Info.ID, "read", "call1", "running", nil, "")
	s.AppendToolPart(sess.ID, am.Info.ID, "read", "call1", "completed", nil, "output1")
	s.AppendTextDelta(sess.ID, am.Info.ID, "text", "after")

	mwp, ok := s.GetMessage(sess.ID, am.Info.ID)
	if !ok {
		t.Fatal("failed to get message")
	}

	wantTypes := []string{"step-start", "text", "tool"}
	wantText := map[int]string{1: "beforeafter"}
	if len(mwp.Parts) != len(wantTypes) {
		t.Fatalf("expected %d parts, got %d", len(wantTypes), len(mwp.Parts))
	}
	for i, want := range wantTypes {
		if mwp.Parts[i].Type != want {
			t.Errorf("part %d want %s, got %s", i, want, mwp.Parts[i].Type)
		}
	}
	for i, want := range wantText {
		if mwp.Parts[i].Text != want {
			t.Errorf("text part %d mismatch: want %q, got %q", i, want, mwp.Parts[i].Text)
		}
	}

}
