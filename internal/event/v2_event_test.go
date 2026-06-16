package event

import (
	"testing"
)

func TestNewSessionNextPrompted(t *testing.T) {
	e := NewSessionNextPrompted("s1", "m1", "hello", "steer")
	if e.Type != TypeSessionNextPrompted {
		t.Errorf("got type %q, want %q", e.Type, TypeSessionNextPrompted)
	}
	if e.ID == "" {
		t.Errorf("ID is empty")
	}
	props, ok := e.Properties.(SessionNextPromptProps)
	if !ok {
		t.Fatalf("wrong properties type")
	}
	if props.SessionID != "s1" || props.MessageID != "m1" || props.Prompt.Text != "hello" || props.Delivery != "steer" {
		t.Errorf("wrong props values: %+v", props)
	}
	if e.GuaranteedDelivery() {
		t.Errorf("should be droppable")
	}
}
