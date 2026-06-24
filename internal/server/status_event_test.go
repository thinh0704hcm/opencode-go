package server

import (
	"strings"
	"testing"

	"github.com/opencode-go/opencode-go/internal/event"
)

func TestSessionStatusEventShape(t *testing.T) {
	// Create a minimal server or just use the bus
	bus := event.NewBus()

	// Subscribe to bus
	sub, cancel := bus.Subscribe()
	defer cancel()

	// Trigger a busy status
	bus.Publish(event.NewSessionStatus("test-session", map[string]string{"type": "busy"}))

	// Read event
	ev := <-sub.Events()

	if ev.Type != event.TypeSessionStatus {
		t.Errorf("expected TypeSessionStatus, got %s", ev.Type)
	}

	props, ok := ev.Properties.(event.SessionStatusProps)
	if !ok {
		t.Fatalf("unexpected properties type: %T", ev.Properties)
	}

	if props.SessionID != "test-session" {
		t.Errorf("expected sessionID test-session, got %s", props.SessionID)
	}

	statusMap, ok := props.Status.(map[string]string)
	if !ok {
		t.Fatalf("unexpected status type: %T", props.Status)
	}

	if statusMap["type"] != "busy" {
		t.Errorf("expected busy status, got %s", statusMap["type"])
	}

	// Verify wire JSON shape via event marshaling
	bytes, err := event.MarshalEvent(ev)
	if err != nil {
		t.Fatal(err)
	}
	expected := `"type":"session.status","properties":{"sessionID":"test-session","status":{"type":"busy"}}`
	if !strings.Contains(string(bytes), expected) {
		t.Errorf("expected to contain %s, got %s", expected, string(bytes))
	}
}
