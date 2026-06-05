package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAI is an OpenAI-compatible streaming client (architecture §3.1).
// POST {baseURL}/chat/completions, Authorization: Bearer <key>, stream:true,
// parse choices[0].delta.content, [DONE] terminator.
type OpenAI struct {
	id      string
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewOpenAI builds an OpenAI-compatible provider.
func NewOpenAI(id, baseURL, apiKey, model string, client *http.Client) *OpenAI {
	if client == nil {
		client = http.DefaultClient
	}
	return &OpenAI{
		id:      id,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  client,
	}
}

// ID returns the provider id.
func (o *OpenAI) ID() string { return o.id }

type chatCompletionsRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// StreamChat opens the provider SSE stream and emits ChatChunks.
func (o *OpenAI) StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error) {
	model := req.Model
	if model == "" {
		model = o.model
	}

	msgs := make([]ChatMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, ChatMessage{Role: "system", Content: req.System})
	}
	msgs = append(msgs, req.Messages...)

	body, err := json.Marshal(chatCompletionsRequest{Model: model, Messages: msgs, Stream: true})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if o.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		resp.Body.Close()
		return nil, fmt.Errorf("provider returned status %d: %s", resp.StatusCode, string(b))
	}

	out := make(chan ChatChunk)
	go func() {
		defer close(out)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				return
			}
			var chunk sseChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue // tolerate non-standard keepalive lines
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			ch := chunk.Choices[0]
			cc := ChatChunk{TextDelta: ch.Delta.Content, ReasoningDelta: ch.Delta.ReasoningContent}
			if ch.FinishReason != nil {
				cc.FinishReason = *ch.FinishReason
			}
			if cc.TextDelta == "" && cc.ReasoningDelta == "" && cc.FinishReason == "" {
				continue
			}
			select {
			case out <- cc:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case out <- ChatChunk{Err: err}:
			case <-ctx.Done():
			}
		}
	}()

	return out, nil
}
