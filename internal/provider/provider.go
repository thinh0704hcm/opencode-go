package provider

import "context"

// ChatMessage is one message in the provider request.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is a streaming chat request (architecture §3.1).
type ChatRequest struct {
	Model    string
	Messages []ChatMessage
	System   string
}

// ChatChunk is one streamed delta from the provider.
type ChatChunk struct {
	TextDelta      string
	ReasoningDelta string
	FinishReason   string
	Err            error
}

// Provider streams chat completions (architecture §3.1).
type Provider interface {
	ID() string
	StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error)
}
