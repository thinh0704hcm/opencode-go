package provider

import (
	"context"
	"encoding/json"
)

// ChatMessage is one message in the provider request.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Optional fields for sending tool results back to the model.
	ToolCallID string `json:"tool_call_id,omitempty"` // when Role=="tool"
	Name       string `json:"name,omitempty"`
	// ToolCalls is set on an assistant message that calls one or more tools.
	// OpenAI requires this assistant message to precede the matching tool
	// result messages (ToolCallID).
	ToolCalls []ChatToolCall `json:"tool_calls,omitempty"`
}

// ChatToolCall is one tool call carried on an assistant ChatMessage.
type ChatToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"` // "function"
	Function ChatToolCallFunction `json:"function"`
}

// ChatToolCallFunction is the function payload of a ChatToolCall.
type ChatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string of args
}

// ToolSchema describes a tool the model may call.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON schema
}

// ToolCall is a complete tool call emitted by the model.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"` // accumulated arguments JSON
}

// ChatRequest is a streaming chat request (architecture §3.1).
type ChatRequest struct {
	Model    string
	Messages []ChatMessage
	System   string
	Tools    []ToolSchema
}

// ChatChunk is one streamed delta from the provider.
type ChatChunk struct {
	TextDelta      string
	ReasoningDelta string
	FinishReason   string
	Err            error
	ToolCall       *ToolCall // non-nil when the model emitted a complete tool call
}

// Provider streams chat completions (architecture §3.1).
type Provider interface {
	ID() string
	StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error)
}
