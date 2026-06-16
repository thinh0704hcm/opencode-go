package server

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/session"
	"github.com/opencode-go/opencode-go/internal/tool"
)

// permTimeout is how long a mutating tool call waits for a permission reply
// before the gate default-denies.
const permTimeout = 90 * time.Second

// runAgentLoop drives the bounded, permission-gated tool-calling loop against an
// ALREADY-CREATED assistant message. The caller is responsible for publishing
// the assistant message + busy status beforehand and for finishGeneration +
// session.idle afterward; this method ONLY performs the provider+tool iteration,
// emitting text/reasoning deltas and tool parts.
// chatHistory builds the provider-visible conversation history for the session.
// Earlier opencode-go sent only the current user message, causing every TUI turn
// to forget the previous one ("i am thinh" -> "who am i?" => "User"). Keep the
// history compact and provider-safe: completed assistant text + user text, with
// images only on the newest user turn.
func (s *Server) chatHistory(sessionID, currentUserMsgID string, currentTexts []string, currentImages []string) []provider.ChatMessage {
	msgs, ok := s.store.Messages(sessionID)
	if !ok || len(msgs) == 0 {
		return []provider.ChatMessage{{Role: "user", Content: provider.MultimodalContent(joinTexts(currentTexts), currentImages)}}
	}

	out := make([]provider.ChatMessage, 0, len(msgs))
	for _, msg := range msgs {
		if msg.Info.Hidden {
			continue
		}
		role := msg.Info.Role
		if role != "user" && role != "assistant" {
			continue
		}
		if role == "assistant" && msg.Info.Time.Completed == nil {
			continue
		}

		if role == "assistant" {
			toolParts := toolPartsOf(msg.Parts)
			if len(toolParts) > 0 {
				// Reconstruct: assistant(tool_calls) + tool results + assistant(final text)
				tcs := make([]provider.ChatToolCall, 0, len(toolParts))
				for _, tp := range toolParts {
					inputJSON, _ := json.Marshal(tp.State.Input)
					tcs = append(tcs, provider.ChatToolCall{
						ID:   tp.CallID,
						Type: "function",
						Function: provider.ChatToolCallFunction{
							Name:      tp.Tool,
							Arguments: string(inputJSON),
						},
					})
				}
				reasoningText := partsText(msg.Parts, "reasoning")
				out = append(out, provider.ChatMessage{
					Role:             "assistant",
					ToolCalls:        tcs,
					ReasoningContent: reasoningText,
				})
				for _, tp := range toolParts {
					output := ""
					if tp.State != nil {
						output = tp.State.Output
					}
					out = append(out, provider.ChatMessage{
						Role: "tool", ToolCallID: tp.CallID, Name: tp.Tool, Content: output,
					})
				}
			}
			// Always include the final text turn (may be empty if purely tool-calling).
			text := partsText(msg.Parts, "text")
			if text != "" || len(toolParts) == 0 {
				var reasoningText string
				if len(toolParts) == 0 {
					reasoningText = partsText(msg.Parts, "reasoning")
				}
				out = append(out, provider.ChatMessage{
					Role:             role,
					Content:          provider.TextContent(text),
					ReasoningContent: reasoningText,
				})
			}
			continue
		}

		text := partsText(msg.Parts, "text")
		content := provider.TextContent(text)
		if role == "user" && msg.Info.ID == currentUserMsgID {
			content = provider.MultimodalContent(text, currentImages)
		}
		out = append(out, provider.ChatMessage{Role: role, Content: content})
	}

	if len(out) == 0 || out[len(out)-1].Role != "user" {
		out = append(out, provider.ChatMessage{Role: "user", Content: provider.MultimodalContent(joinTexts(currentTexts), currentImages)})
	}
	return out
}

func toolPartsOf(parts []session.Part) []session.Part {
	var out []session.Part
	for _, p := range parts {
		if p.Type == "tool" && p.State != nil && p.State.Status != "running" {
			out = append(out, p)
		}
	}
	return out
}

func partsText(parts []session.Part, typ string) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type != typ || p.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(p.Text)
	}
	return b.String()
}

// joinTexts concatenates prompt text parts with newlines.
func joinTexts(texts []string) string {
	var sb strings.Builder
	for i, t := range texts {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(t)
	}
	return sb.String()
}

func (s *Server) runAgentLoop(ctx context.Context, sessionID, messageID, userMsgID, modelID string, texts []string, images []string, callerSystem string, agent Agent, prebuiltMessages ...[]provider.ChatMessage) string {
	var messages []provider.ChatMessage
	if len(prebuiltMessages) > 0 && prebuiltMessages[0] != nil {
		messages = prebuiltMessages[0]
	} else {
		messages = s.chatHistory(sessionID, userMsgID, texts, images)
	}

	sb, err := tool.New(s.workdir)
	if err != nil {
		s.bus.Publish(event.NewSessionError(sessionID, map[string]any{
			"name": "UnknownError",
			"data": map[string]any{"message": err.Error()},
		}))
		return ""
	}

	// Use the actual provider/model from modelID (which may be "providerID/modelID").
	providerID := s.configuredProviderID
	if providerID == "" {
		providerID = s.provider.ID()
	}
	if idx := strings.Index(modelID, "/"); idx > 0 && idx < len(modelID)-1 {
		providerID = modelID[:idx]
		modelID = modelID[idx+1:]
	}

	// commitLen tracks how many entries in messages were present at the start of
	// the current iteration. A mid-stream retry is only safe when no tool
	// results have been appended (commitLen == len(messages)), because executed
	// tool calls have side effects that cannot be undone.
	commitLen := len(messages)

	var prevInput, prevOutput int64

	for {
		s.bus.Publish(event.NewSessionNextStepStarted(sessionID, messageID, agent.Name, modelID, providerID))
		stepStartInput := prevInput
		stepStartOutput := prevOutput

		req := provider.ChatRequest{
			Model:     modelID,
			Messages:  messages,
			System:    combineSystem(buildSystemPrompt(s.workdir, agent.Prompt), callerSystem),
			Tools:     toolSchemas(s.tools, agent.toolAllowed),
			MaxTokens: s.maxTokens,
		}

		var calls []provider.ToolCall
		var finishReason string
		var reasoningID string
		var reasoning strings.Builder
		var textID string
		var textBuf strings.Builder
		var streamErr error

		// streamTimeout caps how long a single StreamChat attempt may run before
		// we consider it hung and retry. 90 s is long enough for a slow reasoning
		// model but short enough that 3 retries stay well under 5 minutes total.
		const streamTimeout = 90 * time.Second
		const maxStreamRetries = 3
		for attempt := 0; attempt < maxStreamRetries; attempt++ {
			if attempt > 0 {
				// Backoff before retry: 2s, 4s.
				select {
				case <-ctx.Done():
					return ""
				case <-time.After(time.Duration(attempt*2) * time.Second):
				}
				// Discard partial content from the failed attempt so the retry
				// starts from a clean slate (no duplicate text in the message).
				s.store.DropTextAndReasoningParts(sessionID, messageID)
				calls = calls[:0]
				finishReason = ""
				reasoningID = ""
				reasoning.Reset()
				textID = ""
				textBuf.Reset()
				s.bus.Publish(event.NewSessionNextRetried(sessionID, attempt, streamErr.Error(), true))
			}

			attemptCtx, attemptCancel := context.WithTimeout(ctx, streamTimeout)
			stream, err := s.provider.StreamChat(attemptCtx, req)
			if err != nil {
				attemptCancel()
				streamErr = err
				if attempt < maxStreamRetries-1 && len(messages) == commitLen {
					continue
				}
				s.bus.Publish(event.NewSessionError(sessionID, map[string]any{
					"name": "UnknownError",
					"data": map[string]any{"message": scrubError(err.Error())},
				}))
				return ""
			}

			streamErr = nil
			for chunk := range stream {
				if chunk.Err != nil {
					streamErr = chunk.Err
					for range stream {
					}
					break
				}
				if chunk.TextDelta != "" {
					if textID == "" {
						textID = event.NewID("txt")
						s.bus.Publish(event.NewSessionNextTextStarted(sessionID, messageID, textID))
					}
					textBuf.WriteString(chunk.TextDelta)
					s.bus.Publish(event.NewSessionNextTextDelta(sessionID, messageID, textID, chunk.TextDelta))
					s.emitDelta(sessionID, messageID, "text", chunk.TextDelta)
				}
				if chunk.ReasoningDelta != "" {
					if reasoningID == "" {
						reasoningID = event.NewID("rsn")
						s.bus.Publish(event.NewSessionNextReasoningStarted(sessionID, messageID, reasoningID))
					}
					reasoning.WriteString(chunk.ReasoningDelta)
					s.bus.Publish(event.NewSessionNextReasoningDelta(sessionID, messageID, reasoningID, chunk.ReasoningDelta))
					s.emitDelta(sessionID, messageID, "reasoning", chunk.ReasoningDelta)
				}
				if chunk.ToolCall != nil {
					calls = append(calls, *chunk.ToolCall)
				}
				if chunk.Usage != nil {
					s.store.SetAssistantUsage(sessionID, messageID, chunk.Usage.Input, chunk.Usage.Output, chunk.Usage.Total)
				}
				if chunk.FinishReason != "" {
					finishReason = chunk.FinishReason
				}
			}

			attemptCancel()

			// Retry on stream error only when no tool results have been committed
			// (no side effects to undo). Give up if retries exhausted.
			if streamErr != nil {
				if attempt < maxStreamRetries-1 && len(messages) == commitLen {
					continue
				}
				s.bus.Publish(event.NewSessionError(sessionID, map[string]any{
					"name": "UnknownError",
					"data": map[string]any{"message": scrubError(streamErr.Error())},
				}))
				return ""
			}
			break // stream completed successfully
		} // end retry loop

		if reasoningID != "" {
			s.bus.Publish(event.NewSessionNextReasoningEnded(sessionID, messageID, reasoningID, reasoning.String()))
			reasoningID = ""
			reasoning.Reset()
		}

		if textID != "" {
			s.bus.Publish(event.NewSessionNextTextEnded(sessionID, messageID, textID, textBuf.String()))
			textID = ""
			textBuf.Reset()
		}

		// No tool calls: the model produced its final text turn.
		if len(calls) == 0 {
			if info, ok := s.store.MessageInfo(sessionID, messageID); ok {
				tok := info.Tokens
				var tokens event.SessionNextStepEndedTokens
				var stepCost float64
				if tok != nil {
					tokens.Input = tok.Input - stepStartInput
					tokens.Output = tok.Output - stepStartOutput
					tokens.Cache.Read = tok.Cache.Read
					tokens.Cache.Write = tok.Cache.Write
					stepCost = computeCost(modelID, tokens.Input, tokens.Output)
					prevInput = tok.Input
					prevOutput = tok.Output
				}
				s.bus.Publish(event.NewSessionNextStepEnded(sessionID, messageID, finishReason, stepCost, tokens))
			}
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
			// Mirror the AI SDK openai-compatible builder: when the turn produced
			// reasoning, echo it as reasoning_content on the assistant message
			// that carries the tool_calls. Providers that reject the field are
			// handled by the provider's strip toggle.
			messages = append(messages, provider.ChatMessage{Role: "assistant", ToolCalls: tcs, ReasoningContent: reasoning.String()})
		}

		for _, call := range calls {
			var toolInput map[string]any
			_ = json.Unmarshal(call.Input, &toolInput)

			isSubtask := call.Name == "delegate" || call.Name == "task"

			if isSubtask {
				prompt, _ := toolInput["prompt"].(string)
				desc, _ := toolInput["description"].(string)
				agentName, _ := toolInput["agent"].(string)
				if agentName == "" {
					agentName = "build"
				}
				modelStr, _ := toolInput["model"].(string)
				// Use the session's actual providerID/modelID for the part display.
				// If the caller specified an override model, parse just the model part.
				partProviderID := providerID
				partModelID := modelID
				if modelStr != "" {
					if idx := strings.Index(modelStr, "/"); idx > 0 && idx < len(modelStr)-1 {
						partProviderID = modelStr[:idx]
						partModelID = modelStr[idx+1:]
					} else {
						partModelID = modelStr
					}
				}
				
				part, _ := s.store.AppendSubtaskPart(sessionID, messageID, prompt, desc, agentName, partProviderID, partModelID, "")
				s.bus.Publish(event.NewMessagePartUpdated(sessionID, part, time.Now().UnixMilli()))
				
				// We ALSO create a standard tool part so state machines understand this is a running operation.
				// By convention, tools that spawn subtasks shouldn't bypass the tool lifecycle entirely.
				// The prompt says: "In internal/server/agent_loop.go, stop bypassing normal ToolPart lifecycle for delegate/task. Treat them as normal tools for state tracking: running -> completed/error via AppendToolPart, publish existing tool events with SDK-required fields if event structs support it."
			}

			s.bus.Publish(event.NewSessionNextToolInputStarted(sessionID, messageID, call.ID, call.Name))
			s.bus.Publish(event.NewSessionNextToolInputEnded(sessionID, messageID, call.ID, string(call.Input)))
			s.bus.Publish(event.NewSessionNextToolCalled(sessionID, messageID, call.ID, call.Name, toolInput))

			part, _ := s.store.AppendToolPart(sessionID, messageID, call.Name, call.ID, "running", toolInput, "")
			s.bus.Publish(event.NewMessagePartUpdated(sessionID, part, time.Now().UnixMilli()))

			if !agent.toolAllowed(call.Name) {
				out := "tool not allowed for this agent: " + call.Name
				p, _ := s.store.AppendToolPart(sessionID, messageID, call.Name, call.ID, "error", toolInput, out)
				s.bus.Publish(event.NewMessagePartUpdated(sessionID, p, time.Now().UnixMilli()))
				messages = append(messages, provider.ChatMessage{Role: "tool", ToolCallID: call.ID, Name: call.Name, Content: out})
				continue
			}

			// For sub-sessions (delegate/task), permSessID is the parent session's
			// ID. This lets us inherit "always allow" grants and show permission
			// dialogs on the parent's TUI panel rather than the invisible sub-session.
			permSessID := permSessionIDFromCtx(ctx)
			if permSessID == "" {
				permSessID = sessionID
			}
			if needsPermission(s.tools, call.Name) &&
				!s.perms.IsAllowed(sessionID, call.Name) &&
				!s.perms.IsAllowed(permSessID, call.Name) {
				preq := s.perms.Ask("per_"+call.ID, permSessID, call.Name)
				// Emit permission.asked after building requestObj below. The TUI expects
				// properties.request.always, not a bare Request.
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
				requestObj := map[string]any{
					"id":         preq.ID,
					"sessionID":  permSessID,
					"permission": call.Name,
					"patterns":   []any{pattern},
					"metadata":   map[string]any{},
					"always":     []any{},
					"tool": map[string]any{
						"messageID": messageID,
						"callID":    call.ID,
					},
				}
				s.bus.Publish(event.NewPermissionAsked(requestObj))

				s.bus.Publish(event.Event{
					ID:   event.NewID("evt"),
					Type: "permission.v2.asked",
					Properties: map[string]any{
						"id":        preq.ID,
						"sessionID": permSessID,
						"action":    call.Name,
						"resources": []string{pattern},
						"metadata":  map[string]any{},
						"source": map[string]any{
							"type":      "tool",
							"messageID": messageID,
							"callID":    call.ID,
						},
					},
				})

				permObj := map[string]any{
					"id":       preq.ID,
					"status":   "pending",
					"request":  requestObj,
					"response": nil,
				}
				// Keep legacy top-level fields for older clients while satisfying
				// opencode 1.16 TUI's permission.request.always access.
				for k, v := range requestObj {
					if k != "tool" {
						permObj[k] = v
					}
				}
				s.bus.Publish(event.NewPermissionUpdated(permObj))
				reply := s.perms.Wait(ctx, preq, permTimeout)
				s.bus.Publish(event.NewPermissionReplied(permSessID, preq.ID, reply))
				s.bus.Publish(event.Event{
					ID:   event.NewID("evt"),
					Type: "permission.v2.replied",
					Properties: map[string]any{
						"sessionID": permSessID,
						"requestID": preq.ID,
						"reply":     reply,
					},
				})
				if reply == "always" {
					s.perms.Allow(permSessID, call.Name)
				}
				if reply == "reject" {
					out := "permission denied"
					p, _ := s.store.AppendToolPart(sessionID, messageID, call.Name, call.ID, "error", toolInput, out)
					s.bus.Publish(event.NewMessagePartUpdated(sessionID, p, time.Now().UnixMilli()))
					messages = append(messages, provider.ChatMessage{Role: "tool", ToolCallID: call.ID, Name: call.Name, Content: out})
					continue
				}
			}

			out, isError := executeToolCall(withSessionID(ctx, sessionID), s.tools, sb, call)
			status := "completed"
			if isError {
				status = "error"
				s.bus.Publish(event.NewSessionNextToolFailed(sessionID, messageID, call.ID, out))
			} else {
				s.bus.Publish(event.NewSessionNextToolSuccess(sessionID, messageID, call.ID, out))
			}
			
			p, _ := s.store.AppendToolPart(sessionID, messageID, call.Name, call.ID, status, toolInput, out)
			s.bus.Publish(event.NewMessagePartUpdated(sessionID, p, time.Now().UnixMilli()))

			s.store.PersistSession(sessionID) // checkpoint: survive a kill mid-turn
			messages = append(messages, provider.ChatMessage{Role: "tool", ToolCallID: call.ID, Name: call.Name, Content: out})
		}
		// Tool results have been committed; the next iteration cannot safely retry
		// from scratch because tool calls have side effects.
		if info, ok := s.store.MessageInfo(sessionID, messageID); ok {
			tok := info.Tokens
			var tokens event.SessionNextStepEndedTokens
			var stepCost float64
			if tok != nil {
				tokens.Input = tok.Input - stepStartInput
				tokens.Output = tok.Output - stepStartOutput
				tokens.Cache.Read = tok.Cache.Read
				tokens.Cache.Write = tok.Cache.Write
				stepCost = computeCost(modelID, tokens.Input, tokens.Output)
				prevInput = tok.Input
				prevOutput = tok.Output
			}
			s.bus.Publish(event.NewSessionNextStepEnded(sessionID, messageID, "tool_calls", stepCost, tokens))
		}
		commitLen = len(messages)
		// Loop continues: the next provider turn sees the tool results.
	}

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
