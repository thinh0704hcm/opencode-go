package event

import (
	"sync/atomic"
	"time"
)

type EventType = string

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
	TypeSessionCreated     = "session.created"
	TypeSessionUpdated     = "session.updated"
	TypeSessionDeleted     = "session.deleted"
	TypeSessionCompact      = "session.compact"
	TypePermissionAsked    = "permission.asked"
	TypePermissionUpdated  = "permission.updated"
	TypePermissionReplied  = "permission.replied"

	TypeSessionNextPrompted         = "session.next.prompted"
	TypeSessionNextPromptAdmitted   = "session.next.prompt.admitted"
	TypeSessionNextPromptPromoted   = "session.next.prompt.promoted"
	TypeSessionNextStepStarted      = "session.next.step.started"
	TypeSessionNextStepEnded        = "session.next.step.ended"
	TypeSessionNextStepFailed       = "session.next.step.failed"
	TypeSessionNextTextStarted      = "session.next.text.started"
	TypeSessionNextTextDelta        = "session.next.text.delta"
	TypeSessionNextTextEnded        = "session.next.text.ended"
	TypeSessionNextToolInputStarted = "session.next.tool.input.started"
	TypeSessionNextToolInputDelta   = "session.next.tool.input.delta"
	TypeSessionNextToolInputEnded   = "session.next.tool.input.ended"
	TypeSessionNextToolCalled       = "session.next.tool.called"
	TypeSessionNextToolSuccess      = "session.next.tool.success"
	TypeSessionNextToolFailed       = "session.next.tool.failed"
	TypeSessionNextRetried          = "session.next.retried"

	TypeSessionNextReasoningStarted = "session.next.reasoning.started"
	TypeSessionNextReasoningDelta   = "session.next.reasoning.delta"
	TypeSessionNextReasoningEnded   = "session.next.reasoning.ended"
)

// Todo event types
const (
	TypeTodoUpdated = "todo.updated"
)

type TodoUpdatedProps struct {
	SessionID string `json:"sessionID"`
	Todos     any    `json:"todos"`
}

func NewTodoUpdated(sessionID string, todos any) Event {
	return Event{ID: newID("evt"), Type: TypeTodoUpdated, Properties: TodoUpdatedProps{SessionID: sessionID, Todos: todos}}
}

// PartDeltaProps is the properties shape for message.part.delta.
// All fields required per architecture §7.1.
type PartDeltaProps struct {
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	PartID    string `json:"partID"`
	Field     string `json:"field"`
	Delta     string `json:"delta"`
}

type SessionNextReasoningStartedProps struct {
	Timestamp          int64  `json:"timestamp"`
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	ReasoningID        string `json:"reasoningID"`
}

type SessionNextReasoningDeltaProps struct {
	Timestamp          int64  `json:"timestamp"`
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	ReasoningID        string `json:"reasoningID"`
	Delta              string `json:"delta"`
}

type SessionNextReasoningEndedProps struct {
	Timestamp          int64  `json:"timestamp"`
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	ReasoningID        string `json:"reasoningID"`
	Text               string `json:"text"`
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

// SessionCreatedProps is the properties shape for session.created.
type SessionCreatedProps struct {
	SessionID string `json:"sessionID"`
	Info      any    `json:"info"`
}

// SessionUpdatedProps is the properties shape for session.updated.
type SessionUpdatedProps struct {
	SessionID string `json:"sessionID"`
	Info      any    `json:"info"`
}

// SessionDeletedProps is the properties shape for session.deleted.
type SessionDeletedProps struct {
	SessionID string `json:"sessionID"`
	Info      any    `json:"info"`
}

// SessionErrorProps is the properties shape for session.error.
type SessionErrorProps struct {
	SessionID string `json:"sessionID"`
	Error     any    `json:"error"`
}

// SessionCompactPayload carries optional block and stats for compression notifications.
type SessionCompactPayload struct {
	SessionID string         `json:"sessionID"`
	Block     any            `json:"block,omitempty"`
	Stats     map[string]any `json:"stats,omitempty"`
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

// NewID exposes the package-internal newID.
func NewID(prefix string) string {
	return newID(prefix)
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

// New creates a generic event with given type and properties.
func New(t EventType, props any) Event {
	return Event{ID: newID("evt"), Type: t, Properties: props}
}

// NewSessionIdle creates a session.idle event (synthetic terminal signal).
func NewSessionIdle(sessionID string) Event {
	return Event{ID: newID("evt"), Type: TypeSessionIdle, Properties: SessionIdleProps{SessionID: sessionID}}
}

// NewSessionStatus creates a session.status event (e.g. {type:"busy"}).
func NewSessionStatus(sessionID string, status any) Event {
	return Event{ID: newID("evt"), Type: TypeSessionStatus, Properties: SessionStatusProps{SessionID: sessionID, Status: status}}
}

// NewSessionCreated creates a session.created event carrying the Session info.
func NewSessionCreated(sessionID string, info any) Event {
	return Event{ID: newID("evt"), Type: TypeSessionCreated,
		Properties: SessionCreatedProps{SessionID: sessionID, Info: info}}
}

// NewSessionUpdated creates a session.updated event carrying the Session info.
func NewSessionUpdated(sessionID string, info any) Event {
	return Event{ID: newID("evt"), Type: TypeSessionUpdated,
		Properties: SessionUpdatedProps{SessionID: sessionID, Info: info}}
}

// NewSessionDeleted creates a session.deleted event carrying the Session info.
func NewSessionDeleted(sessionID string, info any) Event {
	return Event{ID: newID("evt"), Type: TypeSessionDeleted,
		Properties: SessionDeletedProps{SessionID: sessionID, Info: info}}
}

// NewSessionError creates a session.error event.
func NewSessionError(sessionID string, errPayload any) Event {
	return Event{ID: newID("evt"), Type: TypeSessionError, Properties: SessionErrorProps{SessionID: sessionID, Error: errPayload}}
}

// NewSessionCompact creates a session.compact event with optional block and stats.
func NewSessionCompact(sessionID string, block any, stats map[string]any) Event {
	return Event{ID: newID("evt"), Type: TypeSessionCompact, Properties: SessionCompactPayload{SessionID: sessionID, Block: block, Stats: stats}}
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
		TypeSessionStatus, TypeSessionUpdated, TypeSessionDeleted,
		TypeSessionNextPromptAdmitted,
		TypePermissionAsked, TypePermissionUpdated:
		return true
	case TypeMessageUpdated:
		return e.IsFinalAssistant() // only the completed assistant message
	default:
		return false // deltas + everything else are droppable
	}
}

type SessionNextPromptProps struct {
	Timestamp int64  `json:"timestamp"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Prompt    struct {
		Text string `json:"text"`
	} `json:"prompt"`
	Delivery string `json:"delivery"` // "steer" | "queue"
}

type SessionNextStepStartedProps struct {
	Timestamp          int64  `json:"timestamp"`
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	Agent              string `json:"agent"`
	Model              struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
		Variant    string `json:"variant,omitempty"`
	} `json:"model"`
}

type SessionNextStepEndedTokens struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Cache     struct {
		Read  int64 `json:"read"`
		Write int64 `json:"write"`
	} `json:"cache"`
}

type SessionNextStepEndedProps struct {
	Timestamp          int64                      `json:"timestamp"`
	SessionID          string                     `json:"sessionID"`
	AssistantMessageID string                     `json:"assistantMessageID"`
	Finish             string                     `json:"finish"`
	Cost               float64                    `json:"cost"`
	Tokens             SessionNextStepEndedTokens `json:"tokens"`
}

type SessionNextStepFailedProps struct {
	Timestamp          int64  `json:"timestamp"`
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	Error              struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type SessionNextTextStartedProps struct {
	Timestamp          int64  `json:"timestamp"`
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	TextID             string `json:"textID"`
}

type SessionNextTextDeltaProps struct {
	Timestamp          int64  `json:"timestamp"`
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	TextID             string `json:"textID"`
	Delta              string `json:"delta"`
}

type SessionNextTextEndedProps struct {
	Timestamp          int64  `json:"timestamp"`
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	TextID             string `json:"textID"`
	Text               string `json:"text"`
}

type SessionNextToolInputStartedProps struct {
	Timestamp          int64  `json:"timestamp"`
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	CallID             string `json:"callID"`
	Name               string `json:"name"`
}

type SessionNextToolInputDeltaProps struct {
	Timestamp          int64  `json:"timestamp"`
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	CallID             string `json:"callID"`
	Delta              string `json:"delta"`
}

type SessionNextToolInputEndedProps struct {
	Timestamp          int64  `json:"timestamp"`
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	CallID             string `json:"callID"`
	Text               string `json:"text"`
}

type SessionNextToolCalledProps struct {
	Timestamp          int64          `json:"timestamp"`
	SessionID          string         `json:"sessionID"`
	AssistantMessageID string         `json:"assistantMessageID"`
	CallID             string         `json:"callID"`
	Tool               string         `json:"tool"`
	Input              map[string]any `json:"input"`
	Provider           struct {
		Executed bool `json:"executed"`
	} `json:"provider"`
}

type SessionNextToolSuccessProps struct {
	Timestamp          int64          `json:"timestamp"`
	SessionID          string         `json:"sessionID"`
	AssistantMessageID string         `json:"assistantMessageID"`
	CallID             string         `json:"callID"`
	Structured         map[string]any `json:"structured"`
	Content            []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Provider struct {
		Executed bool `json:"executed"`
	} `json:"provider"`
}

type SessionNextToolFailedProps struct {
	Timestamp          int64  `json:"timestamp"`
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	CallID             string `json:"callID"`
	Error              struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
	Provider struct {
		Executed bool `json:"executed"`
	} `json:"provider"`
}

type SessionNextRetriedProps struct {
	Timestamp int64  `json:"timestamp"`
	SessionID string `json:"sessionID"`
	Attempt   int    `json:"attempt"`
	Error     struct {
		Message     string `json:"message"`
		IsRetryable bool   `json:"isRetryable"`
	} `json:"error"`
}

func NewSessionNextPrompted(sessionID, messageID, text, delivery string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextPrompted,
		Properties: SessionNextPromptProps{
			Timestamp: time.Now().UnixMilli(),
			SessionID: sessionID,
			MessageID: messageID,
			Prompt: struct {
				Text string `json:"text"`
			}{Text: text},
			Delivery: delivery,
		},
	}
}

func NewSessionNextPromptAdmitted(sessionID, messageID, text, delivery string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextPromptAdmitted,
		Properties: SessionNextPromptProps{
			Timestamp: time.Now().UnixMilli(),
			SessionID: sessionID,
			MessageID: messageID,
			Prompt: struct {
				Text string `json:"text"`
			}{Text: text},
			Delivery: delivery,
		},
	}
}

func NewSessionNextPromptPromoted(sessionID, messageID, text string, timeCreated int64) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextPromptPromoted,
		Properties: SessionNextPromptProps{
			Timestamp: timeCreated,
			SessionID: sessionID,
			MessageID: messageID,
			Prompt: struct {
				Text string `json:"text"`
			}{Text: text},
			Delivery: "queue",
		},
	}
}

func NewSessionNextStepStarted(sessionID, assistantMsgID, agentName, modelID, providerID string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextStepStarted,
		Properties: SessionNextStepStartedProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: assistantMsgID,
			Agent:              agentName,
			Model: struct {
				ID         string `json:"id"`
				ProviderID string `json:"providerID"`
				Variant    string `json:"variant,omitempty"`
			}{ID: modelID, ProviderID: providerID},
		},
	}
}

func NewSessionNextStepEnded(sessionID, assistantMsgID, finish string, cost float64, tokens SessionNextStepEndedTokens) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextStepEnded,
		Properties: SessionNextStepEndedProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: assistantMsgID,
			Finish:             finish,
			Cost:               cost,
			Tokens:             tokens,
		},
	}
}

func NewSessionNextStepFailed(sessionID, assistantMsgID, errType, errMsg string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextStepFailed,
		Properties: SessionNextStepFailedProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: assistantMsgID,
			Error: struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			}{Type: "unknown", Message: errMsg},
		},
	}
}

func NewSessionNextReasoningStarted(sessionID, messageID, reasoningID string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextReasoningStarted,
		Properties: SessionNextReasoningStartedProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: messageID,
			ReasoningID:        reasoningID,
		},
	}
}

func NewSessionNextReasoningDelta(sessionID, messageID, reasoningID, delta string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextReasoningDelta,
		Properties: SessionNextReasoningDeltaProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: messageID,
			ReasoningID:        reasoningID,
			Delta:              delta,
		},
	}
}

func NewSessionNextReasoningEnded(sessionID, messageID, reasoningID, text string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextReasoningEnded,
		Properties: SessionNextReasoningEndedProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: messageID,
			ReasoningID:        reasoningID,
			Text:               text,
		},
	}
}

func NewSessionNextTextStarted(sessionID, assistantMsgID, textID string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextTextStarted,
		Properties: SessionNextTextStartedProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: assistantMsgID,
			TextID:             textID,
		},
	}
}

func NewSessionNextTextDelta(sessionID, assistantMsgID, textID, delta string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextTextDelta,
		Properties: SessionNextTextDeltaProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: assistantMsgID,
			TextID:             textID,
			Delta:              delta,
		},
	}
}

func NewSessionNextTextEnded(sessionID, assistantMsgID, textID, text string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextTextEnded,
		Properties: SessionNextTextEndedProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: assistantMsgID,
			TextID:             textID,
			Text:               text,
		},
	}
}

func NewSessionNextToolInputStarted(sessionID, assistantMsgID, callID, name string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextToolInputStarted,
		Properties: SessionNextToolInputStartedProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: assistantMsgID,
			CallID:             callID,
			Name:               name,
		},
	}
}

func NewSessionNextToolInputDelta(sessionID, assistantMsgID, callID, delta string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextToolInputDelta,
		Properties: SessionNextToolInputDeltaProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: assistantMsgID,
			CallID:             callID,
			Delta:              delta,
		},
	}
}

func NewSessionNextToolInputEnded(sessionID, assistantMsgID, callID, text string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextToolInputEnded,
		Properties: SessionNextToolInputEndedProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: assistantMsgID,
			CallID:             callID,
			Text:               text,
		},
	}
}

func NewSessionNextToolCalled(sessionID, assistantMsgID, callID, toolName string, input map[string]any) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextToolCalled,
		Properties: SessionNextToolCalledProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: assistantMsgID,
			CallID:             callID,
			Tool:               toolName,
			Input:              input,
			Provider: struct {
				Executed bool `json:"executed"`
			}{Executed: true},
		},
	}
}

func NewSessionNextToolSuccess(sessionID, assistantMsgID, callID, output string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextToolSuccess,
		Properties: SessionNextToolSuccessProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: assistantMsgID,
			CallID:             callID,
			Structured:         map[string]any{},
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{{Type: "text", Text: output}},
			Provider: struct {
				Executed bool `json:"executed"`
			}{Executed: true},
		},
	}
}

func NewSessionNextToolFailed(sessionID, assistantMsgID, callID, errMsg string) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextToolFailed,
		Properties: SessionNextToolFailedProps{
			Timestamp:          time.Now().UnixMilli(),
			SessionID:          sessionID,
			AssistantMessageID: assistantMsgID,
			CallID:             callID,
			Error: struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			}{Type: "unknown", Message: errMsg},
			Provider: struct {
				Executed bool `json:"executed"`
			}{Executed: true},
		},
	}
}

func NewSessionNextRetried(sessionID string, attempt int, errMsg string, isRetryable bool) Event {
	return Event{
		ID:   newID("evt"),
		Type: TypeSessionNextRetried,
		Properties: SessionNextRetriedProps{
			Timestamp: time.Now().UnixMilli(),
			SessionID: sessionID,
			Attempt:   attempt,
			Error: struct {
				Message     string `json:"message"`
				IsRetryable bool   `json:"isRetryable"`
			}{Message: errMsg, IsRetryable: isRetryable},
		},
	}
}

// Command execution events
const TypeCommandExecuted = "command.executed"

type CommandExecutedProps struct {
    Name      string `json:"name"`
    SessionID string `json:"sessionID"`
    Arguments string `json:"arguments"`
    MessageID string `json:"messageID"`
}

func NewCommandExecuted(name, sessionID, arguments, messageID string) Event {
	return New(TypeCommandExecuted, CommandExecutedProps{
		Name: name, SessionID: sessionID, Arguments: arguments, MessageID: messageID,
	})
}

// Shell events
const TypeSessionNextShellStarted = "session.next.shell.started"
const TypeSessionNextShellEnded = "session.next.shell.ended"

type ShellStartedProps struct {
    Timestamp int64  `json:"timestamp"`
    SessionID string `json:"sessionID"`
    MessageID string `json:"messageID"`
    CallID    string `json:"callID"`
    Command   string `json:"command"`
}

func NewSessionNextShellStarted(sessionID, messageID, callID, command string) Event {
	return New(TypeSessionNextShellStarted, ShellStartedProps{
		Timestamp: time.Now().UnixMilli(),
		SessionID: sessionID,
		MessageID: messageID,
		CallID:    callID,
		Command:   command,
	})
}

type ShellEndedProps struct {
    Timestamp int64  `json:"timestamp"`
    SessionID string `json:"sessionID"`
    CallID    string `json:"callID"`
    Output    string `json:"output"`
}

func NewSessionNextShellEnded(sessionID, callID, output string) Event {
	return New(TypeSessionNextShellEnded, ShellEndedProps{
		Timestamp: time.Now().UnixMilli(),
		SessionID: sessionID,
		CallID:    callID,
		Output:    output,
	})
}

// Session diff event
const TypeSessionDiff = "session.diff"

type SessionDiffProps struct {
    SessionID string `json:"sessionID"`
    Diff      any    `json:"diff"`
}

func NewSessionDiff(sessionID string, diff any) Event {
	return New(TypeSessionDiff, SessionDiffProps{
		SessionID: sessionID,
		Diff:      diff,
	})
}
