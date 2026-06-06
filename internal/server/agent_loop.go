package server

import (
	"context"
	"encoding/json"
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
func (s *Server) runAgentLoop(ctx context.Context, sessionID, messageID, modelID string, texts []string) {
	messages := []provider.ChatMessage{{Role: "user", Content: joinTexts(texts)}}

	sb, err := tool.New(s.workdir)
	if err != nil {
		s.bus.Publish(event.NewSessionError(sessionID, map[string]string{"message": err.Error()}))
		return
	}

	for iter := 0; iter < maxAgentIterations; iter++ {
		req := provider.ChatRequest{
			Model:    modelID,
			Messages: messages,
			System:   buildSystemPrompt(s.workdir),
			Tools:    toolSchemas(s.tools),
		}

		stream, err := s.provider.StreamChat(ctx, req)
		if err != nil {
			s.bus.Publish(event.NewSessionError(sessionID, map[string]string{"message": err.Error()}))
			return
		}

		var calls []provider.ToolCall
		for chunk := range stream {
			if chunk.Err != nil {
				s.bus.Publish(event.NewSessionError(sessionID, map[string]string{"message": chunk.Err.Error()}))
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
		}

		// No tool calls: the model produced its final text turn.
		if len(calls) == 0 {
			return
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

			if needsPermission(s.tools, call.Name) {
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
}
