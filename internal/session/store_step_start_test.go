package session

import (
	"testing"
)

func TestAppendStepStart(t *testing.T) {
	s := NewStore()
	sess := s.CreateSession("", "Test", "/work")
	am, ok := s.NewAssistantMessage(sess.ID, "", "provider", "model", "build", "build")
	if !ok {
		t.Fatal("failed to create assistant message")
	}
	// Append second step-start
	part, ok2 := s.AppendStepStart(sess.ID, am.Info.ID)
	if !ok2 {
		t.Fatalf("AppendStepStart returned false")
	}
	if part.Type != "step-start" {
		t.Errorf("expected part type step-start, got %s", part.Type)
	}
	// Verify ordering
	msg, ok3 := s.GetMessage(sess.ID, am.Info.ID)
	if !ok3 {
		t.Fatal("failed to get message after AppendStepStart")
	}
	if len(msg.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(msg.Parts))
	}
	if msg.Parts[0].Type != "step-start" || msg.Parts[1].Type != "step-start" {
		t.Errorf("expected two step-start parts, got %v", []string{msg.Parts[0].Type, msg.Parts[1].Type})
	}
	// Unknown messageID should return false
	if _, ok = s.AppendStepStart(sess.ID, "nonexistent"); ok {
		t.Errorf("expected false for unknown messageID")
	}
}
