package provider

import (
	"context"
	"strings"
)

// ScriptedToolProvider is a tool-calling mock for testing the agent loop
// WITHOUT real tokens. On the first StreamChat invocation it emits a scripted
// set of tool calls; once tool results are appended back (role=="tool"
// messages present) it streams FinalText token-by-token then finishes.
type ScriptedToolProvider struct {
	Calls     []ToolCall
	FinalText string
	callCount int // increments per StreamChat
}

// NewScriptedToolProvider builds a scripted tool-calling mock provider.
func NewScriptedToolProvider(calls []ToolCall, finalText string) *ScriptedToolProvider {
	return &ScriptedToolProvider{Calls: calls, FinalText: finalText}
}

// ID returns the provider id.
func (p *ScriptedToolProvider) ID() string { return "scripted" }

// StreamChat emits scripted tool calls on the first turn, then the final text
// once tool results have been appended to req.Messages.
func (p *ScriptedToolProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error) {
	first := p.callCount == 0 && !hasToolResult(req.Messages)
	p.callCount++

	out := make(chan ChatChunk, len(p.Calls)+len(strings.Fields(p.FinalText))+1)
	calls := p.Calls
	finalText := p.FinalText
	go func() {
		defer close(out)
		if first {
			for i := range calls {
				c := calls[i]
				select {
				case out <- ChatChunk{ToolCall: &c}:
				case <-ctx.Done():
					return
				}
			}
			select {
			case out <- ChatChunk{FinishReason: "tool_calls"}:
			case <-ctx.Done():
			}
			return
		}
		for _, tok := range strings.Fields(finalText) {
			select {
			case out <- ChatChunk{TextDelta: tok + " "}:
			case <-ctx.Done():
				return
			}
		}
		select {
		case out <- ChatChunk{FinishReason: "stop"}:
		case <-ctx.Done():
		}
	}()
	return out, nil
}

// hasToolResult reports whether any message is a tool result (role=="tool").
func hasToolResult(msgs []ChatMessage) bool {
	for _, m := range msgs {
		if m.Role == "tool" {
			return true
		}
	}
	return false
}
