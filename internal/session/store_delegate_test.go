package session

import (
	"testing"
)

func TestUpdateSubtaskTarget(t *testing.T) {
	st := NewStore()
	sess := st.CreateSession("", "Test", "/tmp")
	
	msg, ok := st.NewAssistantMessage(sess.ID, "", "test", "test", "test", "test", false)
	if !ok {
		t.Fatal("failed to create msg")
	}
	
	part, ok := st.AppendSubtaskPart(sess.ID, msg.Info.ID, "test prompt", "desc", "agent", "", "", "")
	if !ok {
		t.Fatal("failed to append subtask part")
	}
	if part.TargetSessionID != "" {
		t.Fatalf("expected empty target session id, got %q", part.TargetSessionID)
	}
	
	updatedPart, ok := st.UpdateSubtaskTarget(sess.ID, msg.Info.ID, "test prompt", "child-123")
	if !ok {
		t.Fatal("failed to update subtask target")
	}
	if updatedPart.TargetSessionID != "child-123" {
		t.Fatalf("expected target session id to be child-123, got %q", updatedPart.TargetSessionID)
	}
	
	// Verify it was actually saved in the store
	msgs, ok := st.Messages(sess.ID)
	if !ok || len(msgs) != 1 {
		t.Fatal("failed to get messages")
	}
    // Note: msgs[0] may have other parts if NewAssistantMessage added them.
    // The appended part should be the last one.
    partsLen := len(msgs[0].Parts)
	if partsLen < 1 {
		t.Fatal("expected at least 1 part")
	}
	if msgs[0].Parts[partsLen-1].TargetSessionID != "child-123" {
		t.Fatalf("expected saved part to have target child-123, got %q", msgs[0].Parts[partsLen-1].TargetSessionID)
	}
}
