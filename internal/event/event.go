package event

import (
	"sync/atomic"
)

// Event is the single canonical event type. The discriminator is Type;
// Properties is type-specific. All wire keys use capital-ID casing
// (sessionID, messageID, partID) per the architecture doc §7.1.
type Event struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Properties any    `json:"properties"`

	// finalAssistant marks a message.updated event as the completed assistant
	// message. Unexported so it is never serialized; drives GuaranteedDelivery.
	finalAssistant bool
}

// Event type discriminators (architecture §7.1).
const (
	TypeServerConnected    = "server.connected"
	TypeMessageUpdated     = "message.updated"
	TypeMessagePartDelta   = "message.part.delta"
	TypeMessagePartUpdated = "message.part.updated"
	TypeSessionIdle        = "session.idle"
	TypeSessionError       = "session.error"
	TypeSessionStatus      = "session.status"
	TypeSessionUpdated     = "session.updated"
	TypeSessionDeleted     = "session.deleted"
	TypePermissionAsked    = "permission.asked"
	TypePermissionUpdated  = "permission.updated"
	TypePermissionReplied  = "permission.replied"
)

// PartDeltaProps is the properties shape for message.part.delta.
// All fields required per architecture §7.1.
type PartDeltaProps struct {
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	PartID    string `json:"partID"`
	Field     string `json:"field"`
	Delta     string `json:"delta"`
}

// PartUpdatedProps is the properties shape for message.part.updated.
// Carries the full cumulative Part snapshot.
type PartUpdatedProps struct {
	SessionID string `json:"sessionID"`
	Part      any    `json:"part"`
	Time      int64  `json:"time"`
}

// MessageUpdatedProps is the properties shape for message.updated.
type MessageUpdatedProps struct {
	SessionID string `json:"sessionID"`
	Info      any    `json:"info"`
}

// SessionIdleProps is the properties shape for session.idle.
type SessionIdleProps struct {
	SessionID string `json:"sessionID"`
}

// SessionStatusProps is the properties shape for session.status.
type SessionStatusProps struct {
	SessionID string `json:"sessionID"`
	Status    any    `json:"status"`
}

// SessionUpdatedProps is the properties shape for session.updated.
type SessionUpdatedProps struct {
	SessionID string `json:"sessionID"`
	Info      any    `json:"info"`
}

// SessionDeletedProps is the properties shape for session.deleted.
type SessionDeletedProps struct {
	Info any `json:"info"`
}

// SessionErrorProps is the properties shape for session.error.
type SessionErrorProps struct {
	SessionID string `json:"sessionID"`
	Error     any    `json:"error"`
}

// PermissionRepliedProps is the properties shape for permission.replied
// (B2-corrected: sessionID, requestID, reply).
type PermissionRepliedProps struct {
	SessionID string `json:"sessionID"`
	RequestID string `json:"requestID"`
	Reply     string `json:"reply"`
}

// monotonic counter for newID.
var idSeq atomic.Uint64

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// newID produces prefix + "_" + base62(monotonic). Architecture §7.1.
func newID(prefix string) string {
	n := idSeq.Add(1)
	return prefix + "_" + base62(n)
}

func base62(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = base62Alphabet[n%62]
		n /= 62
	}
	return string(buf[i:])
}

// NewServerConnected creates a server.connected event with EMPTY properties.
func NewServerConnected() Event {
	return Event{ID: newID("evt"), Type: TypeServerConnected, Properties: struct{}{}}
}

// NewSessionIdle creates a session.idle event (synthetic terminal signal).
func NewSessionIdle(sessionID string) Event {
	return Event{ID: newID("evt"), Type: TypeSessionIdle, Properties: SessionIdleProps{SessionID: sessionID}}
}

// NewSessionStatus creates a session.status event (e.g. {type:"busy"}).
func NewSessionStatus(sessionID string, status any) Event {
	return Event{ID: newID("evt"), Type: TypeSessionStatus, Properties: SessionStatusProps{SessionID: sessionID, Status: status}}
}

// NewSessionUpdated creates a session.updated event carrying the Session info.
func NewSessionUpdated(sessionID string, info any) Event {
	return Event{ID: newID("evt"), Type: TypeSessionUpdated,
		Properties: SessionUpdatedProps{SessionID: sessionID, Info: info}}
}

// NewSessionDeleted creates a session.deleted event carrying the Session info.
func NewSessionDeleted(info any) Event {
	return Event{ID: newID("evt"), Type: TypeSessionDeleted,
		Properties: SessionDeletedProps{Info: info}}
}

// NewSessionError creates a session.error event.
func NewSessionError(sessionID string, errPayload any) Event {
	return Event{ID: newID("evt"), Type: TypeSessionError, Properties: SessionErrorProps{SessionID: sessionID, Error: errPayload}}
}

// NewMessagePartDelta creates a message.part.delta event (DROPPABLE).
func NewMessagePartDelta(sessionID, messageID, partID, field, delta string) Event {
	return Event{ID: newID("evt"), Type: TypeMessagePartDelta,
		Properties: PartDeltaProps{SessionID: sessionID, MessageID: messageID, PartID: partID, Field: field, Delta: delta}}
}

// NewMessagePartUpdated creates a message.part.updated event with the full part snapshot.
func NewMessagePartUpdated(sessionID string, part any, t int64) Event {
	return Event{ID: newID("evt"), Type: TypeMessagePartUpdated,
		Properties: PartUpdatedProps{SessionID: sessionID, Part: part, Time: t}}
}

// NewMessageUpdated creates a message.updated event carrying the Message info.
// finalAssistant marks whether this is the completed assistant message (drives
// guaranteed-delivery classification, §2.3).
func NewMessageUpdated(sessionID string, info any, finalAssistant bool) Event {
	return Event{ID: newID("evt"), Type: TypeMessageUpdated,
		Properties:     MessageUpdatedProps{SessionID: sessionID, Info: info},
		finalAssistant: finalAssistant}
}

// NewPermissionReplied creates a permission.replied event (B2 shape).
func NewPermissionReplied(sessionID, requestID, reply string) Event {
	return Event{ID: newID("evt"), Type: TypePermissionReplied,
		Properties: PermissionRepliedProps{SessionID: sessionID, RequestID: requestID, Reply: reply}}
}

// NewPermissionAsked creates a permission.asked event; properties is the
// PermissionRequest object directly.
func NewPermissionAsked(req any) Event {
	return Event{ID: newID("evt"), Type: TypePermissionAsked, Properties: req}
}

// NewPermissionUpdated creates a permission.updated event; properties is the
// Permission object directly (the opencode TUI consumes this shape).
func NewPermissionUpdated(perm any) Event {
	return Event{ID: newID("evt"), Type: TypePermissionUpdated, Properties: perm}
}

// IsFinalAssistant reports whether this message.updated event is the completed
// assistant message (used by GuaranteedDelivery).
func (e Event) IsFinalAssistant() bool {
	return e.finalAssistant
}

// GuaranteedDelivery reports whether this event must not be silently dropped
// under backpressure (architecture §2.3). Classification lives next to the
// constructors so the policy cannot drift from the type set.
func (e Event) GuaranteedDelivery() bool {
	switch e.Type {
	case TypeSessionIdle, TypeSessionError,
		TypePermissionAsked, TypePermissionUpdated:
		return true
	case TypeMessageUpdated:
		return e.IsFinalAssistant() // only the completed assistant message
	default:
		return false // deltas + everything else are droppable
	}
}
