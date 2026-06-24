package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
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
    extraHeaders map[string]string
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

// SetExtraHeaders assigns custom headers for the provider.
func (o *OpenAI) SetExtraHeaders(h map[string]string) {
    o.extraHeaders = h
}

// ID returns the provider id.
func (o *OpenAI) ID() string { return o.id }

// BaseURL returns the configured base URL (e.g. http://host:port/v1). Used to
// source the 9Router web search/fetch endpoints, which share this gateway.
func (o *OpenAI) BaseURL() string { return o.baseURL }

// APIKey returns the configured API key (shared with the 9Router gateway).
func (o *OpenAI) APIKey() string { return o.apiKey }

type chatCompletionsRequest struct {
	Model           string         `json:"model"`
	Messages        []ChatMessage  `json:"messages"`
	Stream          bool           `json:"stream"`
	StreamOptions   *streamOptions `json:"stream_options,omitempty"`
	Tools           []chatTool     `json:"tools,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	// MaxTokens is omitted when 0 so non-budgeted turns stay byte-identical and
	// invalid (< 1) budgets never reach the upstream.
	MaxTokens int `json:"max_tokens,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content          string             `json:"content"`
			ReasoningContent string             `json:"reasoning_content"`
			ToolCalls        []sseToolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		TotalTokens         int `json:"total_tokens"`
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
		CompletionTokensDetails *struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details,omitempty"`
	} `json:"usage"`
	// Error is set by some proxies (e.g. 9Router) when a backend fails mid-stream.
	// Without this field the error JSON would be silently dropped as an empty chunk.
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
		Type    string `json:"type"`
	} `json:"error"`
}

type sseToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type accumulatingToolCall struct {
	id   string
	name string
	args strings.Builder
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

	// Clamp the output budget: emit only a valid (>= 1) max_tokens. A value < 1
	// is dropped (omitempty) rather than forwarded, which is exactly the bug
	// that makes the upstream return "max_tokens must be at least 1".
	maxTokens := req.MaxTokens
	if maxTokens < 1 {
		maxTokens = 0
	}

	var tools []chatTool
	if len(req.Tools) > 0 {
		tools = make([]chatTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			params := t.Parameters
			if params == nil {
				params = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, chatTool{
				Type: "function",
				Function: chatToolFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  params,
				},
			})
		}
	}

	payload := chatCompletionsRequest{Model: model, Messages: msgs, Stream: true, StreamOptions: &streamOptions{IncludeUsage: true}, Tools: tools, ReasoningEffort: req.ReasoningEffort, MaxTokens: maxTokens}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if len(req.ExtraBody) > 0 {
		var merged map[string]any
		if err := json.Unmarshal(body, &merged); err != nil {
			return nil, err
		}
		for k, v := range req.ExtraBody {
			merged[k] = v
		}
		body, err = json.Marshal(merged)
		if err != nil {
			return nil, err
		}
	}

	// Retry transient provider failures (transport errors and retryable
	// statuses such as 429/5xx) up to maxAttempts, mirroring the TUI behavior.
	// The body is consumed by Do, so the request is rebuilt on every attempt.
	const maxAttempts = 3
	var resp *http.Response
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		if o.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
		}
		// Apply custom extra headers
		for k, v := range o.extraHeaders {
			httpReq.Header.Set(k, v)
		}

		r, err := o.client.Do(httpReq)
		if err != nil {
			// Transport error: retryable.
			lastErr = err
			if attempt < maxAttempts {
				if werr := retryBackoff(ctx, attempt, ""); werr != nil {
					return nil, werr
				}
				continue
			}
			return nil, err
		}

		if r.StatusCode == http.StatusOK {
			resp = r
			break
		}

		if isRetryableStatus(r.StatusCode) {
			b, _ := io.ReadAll(io.LimitReader(r.Body, 8192))
			retryAfter := r.Header.Get("Retry-After")
			r.Body.Close()
			lastErr = fmt.Errorf("provider returned status %d: %s", r.StatusCode, string(b))
			if attempt < maxAttempts {
				if werr := retryBackoff(ctx, attempt, retryAfter); werr != nil {
					return nil, werr
				}
				continue
			}
			return nil, lastErr
		}

		// Non-retryable non-2xx (e.g. 400/401/403/404): fail immediately.
		b, _ := io.ReadAll(io.LimitReader(r.Body, 8192))
		r.Body.Close()
		return nil, fmt.Errorf("provider returned status %d: %s", r.StatusCode, string(b))
	}
	if resp == nil {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("provider request failed after %d attempts", maxAttempts)
	}

	out := make(chan ChatChunk)
	go func() {
		defer close(out)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		toolCalls := map[int]*accumulatingToolCall{}
		var toolOrder []int

		emitToolCalls := func() bool {
			for _, idx := range toolOrder {
				acc := toolCalls[idx]
				tc := ChatChunk{ToolCall: &ToolCall{
					ID:    acc.id,
					Name:  acc.name,
					Input: json.RawMessage(acc.args.String()),
				}}
				select {
				case out <- tc:
				case <-ctx.Done():
					return false
				}
			}
			return true
		}

		sawDone := false
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
				sawDone = true
				if len(toolOrder) > 0 {
					if !emitToolCalls() {
						return
					}
				}
				// Emit final stop chunk per protocol
				select {
				case out <- ChatChunk{FinishReason: "stop"}:
				case <-ctx.Done():
					return
				}
				return
			}

			var chunk sseChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue // tolerate non-standard keepalive lines
			}

			// Surface mid-stream errors from proxies (e.g. 9Router backend failure).
			if chunk.Error != nil && chunk.Error.Message != "" {
				select {
				case out <- ChatChunk{Err: fmt.Errorf("provider error: %s", chunk.Error.Message)}:
				case <-ctx.Done():
				}
				return
			}

			// A usage object usually arrives on the final chunk, which may carry
			// empty choices. Emit a usage-only ChatChunk so the turn can record
			// token accounting even when there is no text/reasoning/finish.
			var usage *Usage
			if chunk.Usage != nil {
				usage = &Usage{
					Input:      chunk.Usage.PromptTokens,
					Output:     chunk.Usage.CompletionTokens,
					Total:      chunk.Usage.TotalTokens,
					Reasoning:  0,
					CacheRead:  0,
					CacheWrite: 0,
				}
				if chunk.Usage.PromptTokensDetails != nil {
					usage.CacheRead = chunk.Usage.PromptTokensDetails.CachedTokens
				}
				if chunk.Usage.CompletionTokensDetails != nil {
					usage.Reasoning = chunk.Usage.CompletionTokensDetails.ReasoningTokens
				}

			}

			if len(chunk.Choices) == 0 {
				if usage != nil {
					select {
					case out <- ChatChunk{Usage: usage}:
					case <-ctx.Done():
						return
					}
				}
				continue
			}
			ch := chunk.Choices[0]

			for _, tcd := range ch.Delta.ToolCalls {
				acc := toolCalls[tcd.Index]
				if acc == nil {
					acc = &accumulatingToolCall{}
					toolCalls[tcd.Index] = acc
					toolOrder = append(toolOrder, tcd.Index)
				}
				if tcd.ID != "" {
					acc.id = tcd.ID
				}
				if tcd.Function.Name != "" {
					acc.name = tcd.Function.Name
				}
				if tcd.Function.Arguments != "" {
					acc.args.WriteString(tcd.Function.Arguments)
				}
			}

			cc := ChatChunk{TextDelta: ch.Delta.Content, ReasoningDelta: ch.Delta.ReasoningContent}
			if ch.FinishReason != nil {
				cc.FinishReason = *ch.FinishReason
				sawDone = true
			}

			if cc.FinishReason == "tool_calls" && len(toolOrder) > 0 {
				if !emitToolCalls() {
					return
				}
				select {
				case out <- ChatChunk{FinishReason: "tool_calls"}:
				case <-ctx.Done():
					return
				}
				toolCalls = map[int]*accumulatingToolCall{}
				toolOrder = nil
				continue
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
		if len(toolOrder) > 0 {
			if !emitToolCalls() {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case out <- ChatChunk{Err: err}:
			case <-ctx.Done():
			}
		}
		if !sawDone {
			select {
			case out <- ChatChunk{Err: fmt.Errorf("stream closed without [DONE]")}:
			case <-ctx.Done():
				return
			}
		}

	}()

	return out, nil
}

// isRetryableStatus reports whether an HTTP status warrants a retry.
func isRetryableStatus(code int) bool {
	return code == 429 || code == 500 || code == 502 || code == 503 || code == 504
}

// retryBackoff waits before the next attempt, honoring a small Retry-After
// header when present and aborting on context cancellation/timeout.
func retryBackoff(ctx context.Context, attempt int, retryAfter string) error {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if retryAfter != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && secs > 0 {
			ra := time.Duration(secs) * time.Second
			if ra > 5*time.Second {
				ra = 5 * time.Second
			}
			d = ra
		}
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
