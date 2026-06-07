package provider

import (
	"context"
	"encoding/json"
)

// ChatMessage is one message in the provider request.
type ChatMessage struct {
	Role string `json:"role"`
	// Content is either a plain string (the common case) or a multimodal
	// content array ([]ContentPart) for vision models. A string marshals to a
	// JSON string (unchanged wire format); a []ContentPart marshals to an array.
	Content any `json:"content,omitempty"`
	// Optional fields for sending tool results back to the model.
	ToolCallID string `json:"tool_call_id,omitempty"` // when Role=="tool"
	Name       string `json:"name,omitempty"`
	// ToolCalls is set on an assistant message that calls one or more tools.
	// OpenAI requires this assistant message to precede the matching tool
	// result messages (ToolCallID).
	ToolCalls []ChatToolCall `json:"tool_calls,omitempty"`
}

// ContentPart is one element of a multimodal message content array (OpenAI shape).
type ContentPart struct {
	Type     string        `json:"type"` // "text" | "image_url"
	Text     string        `json:"text,omitempty"`
	ImageURL *ContentImage `json:"image_url,omitempty"`
}

// ContentImage is the image_url payload (URL may be a data: URI with base64).
type ContentImage struct {
	URL string `json:"url"`
}

// TextContent returns a plain string content (the common case).
func TextContent(s string) any { return s }

// MultimodalContent builds a content array of a text prompt plus image data URIs.
// dataURLs are full "data:image/...;base64,..." strings. If imgs is empty it
// returns the plain string so non-image turns stay byte-identical on the wire.
func MultimodalContent(text string, dataURLs []string) any {
	if len(dataURLs) == 0 {
		return text
	}
	parts := make([]ContentPart, 0, len(dataURLs)+1)
	if text != "" {
		parts = append(parts, ContentPart{Type: "text", Text: text})
	}
	for _, u := range dataURLs {
		parts = append(parts, ContentPart{Type: "image_url", ImageURL: &ContentImage{URL: u}})
	}
	return parts
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

// Usage carries token accounting parsed from a provider stream's usage object.
type Usage struct {
	Input  int
	Output int
	Total  int
}

// ChatChunk is one streamed delta from the provider.
type ChatChunk struct {
	TextDelta      string
	ReasoningDelta string
	FinishReason   string
	Err            error
	ToolCall       *ToolCall // non-nil when the model emitted a complete tool call
	Usage          *Usage    // non-nil when the chunk carries token usage (usually final)
}

// Provider streams chat completions (architecture §3.1).
type Provider interface {
	ID() string
	StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error)
}
