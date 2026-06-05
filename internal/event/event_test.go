package event

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestServerConnectedEmptyProperties(t *testing.T) {
	ev := NewServerConnected()
	if ev.Type != TypeServerConnected {
		t.Fatalf("type = %q, want %q", ev.Type, TypeServerConnected)
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if string(m["properties"]) != "{}" {
		t.Fatalf("properties = %s, want {}", m["properties"])
	}
}

func TestPartDeltaCapitalIDKeys(t *testing.T) {
	ev := NewMessagePartDelta("ses_1", "msg_1", "prt_1", "text", "hi")
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Type       string `json:"type"`
		Properties struct {
			SessionID string `json:"sessionID"`
			MessageID string `json:"messageID"`
			PartID    string `json:"partID"`
			Field     string `json:"field"`
			Delta     string `json:"delta"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeMessagePartDelta {
		t.Fatalf("type = %q", got.Type)
	}
	p := got.Properties
	if p.SessionID != "ses_1" || p.MessageID != "msg_1" || p.PartID != "prt_1" || p.Field != "text" || p.Delta != "hi" {
		t.Fatalf("properties mismatch: %+v", p)
	}

	// Assert the literal capital-ID keys are present (catches casing drift).
	for _, key := range []string{`"sessionID"`, `"messageID"`, `"partID"`, `"field"`, `"delta"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("missing key %s in %s", key, b)
		}
	}
}

func TestSessionIdleShape(t *testing.T) {
	ev := NewSessionIdle("ses_abc")
	b, _ := json.Marshal(ev)
	var got struct {
		Type       string `json:"type"`
		Properties struct {
			SessionID string `json:"sessionID"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeSessionIdle || got.Properties.SessionID != "ses_abc" {
		t.Fatalf("session.idle shape wrong: %s", b)
	}
}

func TestPermissionRepliedShape(t *testing.T) {
	ev := NewPermissionReplied("ses_1", "per_1", "once")
	b, _ := json.Marshal(ev)
	for _, key := range []string{`"sessionID"`, `"requestID"`, `"reply"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("missing key %s in %s", key, b)
		}
	}
}

func TestGuaranteedDeliveryClassification(t *testing.T) {
	cases := []struct {
		ev   Event
		want bool
	}{
		{NewSessionIdle("ses_1"), true},
		{NewSessionError("ses_1", nil), true},
		{NewPermissionAsked(struct{}{}), true},
		{NewMessagePartDelta("ses_1", "msg_1", "prt_1", "text", "x"), false},
		{NewMessagePartUpdated("ses_1", nil, 0), false},
		{NewMessageUpdated("ses_1", nil, false), false}, // non-final assistant/user
		{NewMessageUpdated("ses_1", nil, true), true},   // final assistant
		{NewServerConnected(), false},
	}
	for i, c := range cases {
		if got := c.ev.GuaranteedDelivery(); got != c.want {
			t.Fatalf("case %d (%s): GuaranteedDelivery = %v, want %v", i, c.ev.Type, got, c.want)
		}
	}
}
