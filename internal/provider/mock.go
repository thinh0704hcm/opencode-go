package provider

import (
	"context"
	"strings"
)

// Mock streams a fixed short reply token-by-token so M1 is testable WITHOUT
// burning real API tokens (env OPENCODE_GO_MOCK=1). Architecture: provider
// section requires a mock for tokenless testing.
type Mock struct {
	id    string
	reply string
}

// DefaultMockReply is the canned reply streamed by the mock provider.
const DefaultMockReply = "Hello from opencode-go mock provider."

// NewMock builds a mock provider. If reply is empty, DefaultMockReply is used.
func NewMock(reply string) *Mock {
	if reply == "" {
		reply = DefaultMockReply
	}
	return &Mock{id: "mock", reply: reply}
}

// ID returns the provider id.
func (m *Mock) ID() string { return m.id }

// StreamChat emits the canned reply token-by-token (whitespace-delimited),
// preserving spaces, then a finish chunk.
func (m *Mock) StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error) {
	out := make(chan ChatChunk)
	go func() {
		defer close(out)
		tokens := tokenize(m.reply)
		for _, tok := range tokens {
			select {
			case out <- ChatChunk{TextDelta: tok}:
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

// tokenize splits s into word tokens, keeping the trailing space on each token
// so the reassembled text equals the original.
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	fields := strings.Fields(s)
	out := make([]string, 0, len(fields))
	for i, f := range fields {
		if i < len(fields)-1 {
			out = append(out, f+" ")
		} else {
			out = append(out, f)
		}
	}
	return out
}
