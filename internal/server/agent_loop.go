package server

import (
	"context"
	"encoding/json"
	"regexp"
	"time"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/tool"
)

// maxAgentIterations bounds the provider<->tool loop so a misbehaving model can
// never spin forever (the whole point of this design vs. the Node pathology).
const maxAgentIterations = 16

// permTimeout is how long a mutating tool call waits for a permission reply
// before the gate default-denies.
const permTimeout = 60 * time.Second

// runAgentLoop drives the bounded, permission-gated tool-calling loop against an
// ALREADY-CREATED assistant message. The caller is responsible for publishing
// the assistant message + busy status beforehand and for finishGeneration +
// session.idle afterward; this method ONLY performs the provider+tool iteration,
// emitting text/reasoning deltas and tool parts.
func (s *Server) runAgentLoop(ctx context.Context, sessionID, messageID, modelID string, texts []string, callerSystem string, agent Agent) string {
	messages := []provider.ChatMessage{{Role: "user", Content: joinTexts(texts)}}

	sb, err := tool.New(s.workdir)
	if err != nil {
		s.bus.Publish(event.NewSessionError(sessionID, map[string]string{"message": err.Error()}))
		return ""
	}

	for iter := 0; iter < maxAgentIterations; iter++ {
		req := provider.ChatRequest{
			Model:    modelID,
			Messages: messages,
			System:   combineSystem(buildSystemPrompt(s.workdir, agent.Prompt), callerSystem),
			Tools:    toolSchemas(s.tools, agent.toolAllowed),
		}

		stream, err := s.provider.StreamChat(ctx, req)
		if err != nil {
			s.bus.Publish(event.NewSessionError(sessionID, map[string]string{"message": scrubError(err.Error())}))
			return ""
		}

		var calls []provider.ToolCall
		var finishReason string
		for chunk := range stream {
			if chunk.Err != nil {
				s.bus.Publish(event.NewSessionError(sessionID, map[string]string{"message": scrubError(chunk.Err.Error())}))
				continue
			}
			if chunk.TextDelta != "" {
				s.emitDelta(sessionID, messageID, "text", chunk.TextDelta)
			}
			if chunk.ReasoningDelta != "" {
				s.emitDelta(sessionID, messageID, "reasoning", chunk.ReasoningDelta)
			}
			if chunk.ToolCall != nil {
				calls = append(calls, *chunk.ToolCall)
			}
			if chunk.Usage != nil {
				// Record token accounting on the assistant message. Last usage
				// chunk wins for a multi-step turn (each step's final chunk
				// carries cumulative usage from the provider).
				s.store.SetAssistantUsage(sessionID, messageID, chunk.Usage.Input, chunk.Usage.Output, chunk.Usage.Total)
			}
			if chunk.FinishReason != "" {
				finishReason = chunk.FinishReason
			}
		}

		// No tool calls: the model produced its final text turn.
		if len(calls) == 0 {
			return finishReason
		}

		// OpenAI protocol: the assistant message carrying the tool_calls MUST
		// precede the matching tool result messages. Append it before executing
		// the calls so the next turn sees a valid sequence.
		if len(calls) > 0 {
			tcs := make([]provider.ChatToolCall, 0, len(calls))
			for _, c := range calls {
				tcs = append(tcs, provider.ChatToolCall{
					ID:       c.ID,
					Type:     "function",
					Function: provider.ChatToolCallFunction{Name: c.Name, Arguments: string(c.Input)},
				})
			}
			messages = append(messages, provider.ChatMessage{Role: "assistant", ToolCalls: tcs})
		}

		for _, call := range calls {
			var toolInput map[string]any
			_ = json.Unmarshal(call.Input, &toolInput)
			part, _ := s.store.AppendToolPart(sessionID, messageID, call.Name, call.ID, "running", toolInput, "")
			s.bus.Publish(event.NewMessagePartUpdated(sessionID, part, time.Now().UnixMilli()))

			if !agent.toolAllowed(call.Name) {
				out := "tool not allowed for this agent: " + call.Name
				p, _ := s.store.AppendToolPart(sessionID, messageID, call.Name, call.ID, "error", toolInput, out)
				s.bus.Publish(event.NewMessagePartUpdated(sessionID, p, time.Now().UnixMilli()))
				messages = append(messages, provider.ChatMessage{Role: "tool", ToolCallID: call.ID, Name: call.Name, Content: out})
				continue
			}

			if needsPermission(s.tools, call.Name) && !s.perms.IsAllowed(sessionID, call.Name) {
				preq := s.perms.Ask("per_"+call.ID, sessionID, call.Name)
				s.bus.Publish(event.NewPermissionAsked(preq))
				// Also emit permission.updated with a Permission-shaped object so
				// the opencode 1.16.0 TUI (which listens for permission.updated)
				// renders an approve prompt. id MUST equal preq.ID so the TUI's
				// reply routes back to the gate.
				pattern := string(call.Input)
				var args map[string]any
				if json.Unmarshal(call.Input, &args) == nil {
					if v, ok := args["command"].(string); ok && v != "" {
						pattern = v
					} else if v, ok := args["path"].(string); ok && v != "" {
						pattern = v
					}
				}
				permObj := map[string]any{
					"id":        preq.ID,
					"type":      call.Name,
					"pattern":   pattern,
					"always":    []any{},        // TUI reads request.always.length — MUST be an array
					"patterns":  []any{pattern}, // real PermissionRequest uses a patterns array
					"sessionID": sessionID,
					"messageID": messageID,
					"callID":    call.ID,
					"title":     "Allow tool: " + call.Name,
					"metadata":  map[string]any{},
					"time":      map[string]any{"created": time.Now().UnixMilli()},
				}
				s.bus.Publish(event.NewPermissionUpdated(permObj))
				reply := s.perms.Wait(ctx, preq, permTimeout)
				s.bus.Publish(event.NewPermissionReplied(sessionID, preq.ID, reply))
				if reply == "always" {
					s.perms.Allow(sessionID, call.Name)
				}
				if reply == "reject" {
					out := "permission denied"
					p, _ := s.store.AppendToolPart(sessionID, messageID, call.Name, call.ID, "error", toolInput, out)
					s.bus.Publish(event.NewMessagePartUpdated(sessionID, p, time.Now().UnixMilli()))
					messages = append(messages, provider.ChatMessage{Role: "tool", ToolCallID: call.ID, Name: call.Name, Content: out})
					continue
				}
			}

			out, isError := executeToolCall(ctx, s.tools, sb, call)
			status := "completed"
			if isError {
				status = "error"
			}
			p, _ := s.store.AppendToolPart(sessionID, messageID, call.Name, call.ID, status, toolInput, out)
			s.bus.Publish(event.NewMessagePartUpdated(sessionID, p, time.Now().UnixMilli()))
			messages = append(messages, provider.ChatMessage{Role: "tool", ToolCallID: call.ID, Name: call.Name, Content: out})
		}
		// Loop continues: the next provider turn sees the tool results.
	}

	// Exhausted the iteration budget without a final text turn.
	s.bus.Publish(event.NewSessionError(sessionID, map[string]string{"message": "max tool iterations reached"}))
	return ""
}

// combineSystem appends a caller-supplied system string after the built-in
// base prompt (matching how opencode appends env/instructions after the base
// prompt). Returns base unchanged when extra is empty.
func combineSystem(base, extra string) string {
	if extra == "" {
		return base
	}
	return base + "\n\n" + extra
}

// scrubError redacts secrets that some gateways echo back inside 4xx error
// bodies before the message is broadcast to every SSE subscriber. It strips
// bearer tokens and sk-/AIza style API keys and caps the length to avoid
// dumping huge response bodies.
var (
	scrubBearerRe = regexp.MustCompile(`(?i)bearer\s+\S+`)
	scrubSkRe     = regexp.MustCompile(`sk-[A-Za-z0-9_\-]{8,}`)
	scrubAizaRe   = regexp.MustCompile(`AIza[0-9A-Za-z_\-]{20,}`)
)

func scrubError(msg string) string {
	msg = scrubBearerRe.ReplaceAllString(msg, "Bearer ***")
	msg = scrubSkRe.ReplaceAllString(msg, "sk-***")
	msg = scrubAizaRe.ReplaceAllString(msg, "AIza***")
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return msg
}
