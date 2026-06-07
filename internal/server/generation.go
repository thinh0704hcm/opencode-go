package server

import (
	"context"
	"time"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/session"
)

// publishUserMessage publishes message.updated for the user message and a
// message.part.updated for each of its parts (the TUI renders message text
// from message.part.updated events, not from message.updated info). The info
// update is published before the parts so ordering holds for the consumer.
func (s *Server) publishUserMessage(sessionID string, msg session.MessageWithParts) {
	s.bus.Publish(event.NewMessageUpdated(sessionID, msg.Info, false))
	for i := range msg.Parts {
		s.bus.Publish(event.NewMessagePartUpdated(sessionID, msg.Parts[i], time.Now().UnixMilli()))
	}
}

// publishPermissionReplied publishes the permission.replied event (B2 shape).
func (s *Server) publishPermissionReplied(sessionID, requestID, reply string) {
	s.bus.Publish(event.NewPermissionReplied(sessionID, requestID, reply))
}

// runGeneration runs one assistant turn for a session and emits the locked
// terminal-contract event sequence (architecture §2.4, Option A):
//
//	session.status{busy}
//	-> message.updated(assistant, time.completed=null)
//	-> message.part.delta (field text) AND message.part.updated (full snapshot)...
//	-> message.updated(assistant, time.completed set)   [GUARANTEED]
//	-> session.idle{sessionID}                          [GUARANTEED, synthetic]
//
// (message.updated(user) is published by the handler before this runs.)
func (s *Server) runGeneration(sessionID, userMsgID, providerID, modelID string, texts []string, callerSystem string) {
	s.runGenerationSync(sessionID, userMsgID, providerID, modelID, texts, callerSystem)
}

// runGenerationSync runs the assistant turn inline (same pipeline and event
// sequence as runGeneration) and returns the final assistant MessageWithParts
// once the turn has completed. ok is false if the session/message could not be
// resolved. The async path wraps this in a goroutine; the synchronous
// POST /session/{id}/message handler blocks on it directly.
func (s *Server) runGenerationSync(sessionID, userMsgID, providerID, modelID string, texts []string, callerSystem string) (session.MessageWithParts, bool) {
	ctx, cancel := context.WithCancel(context.Background())
	s.registerCancel(sessionID, cancel)
	defer func() { s.clearCancel(sessionID); cancel() }()

	// session.status{type:"busy"}
	s.bus.Publish(event.NewSessionStatus(sessionID, map[string]string{"type": "busy"}))

	// Assistant message (time.completed=null) + message.updated(assistant).
	asst, ok := s.store.NewAssistantMessage(sessionID, userMsgID, providerID, modelID)
	if !ok {
		s.bus.Publish(event.NewSessionError(sessionID, map[string]string{"message": "session not found"}))
		s.bus.Publish(event.NewSessionStatus(sessionID, map[string]string{"type": "idle"}))
		s.bus.Publish(event.NewSessionIdle(sessionID))
		return session.MessageWithParts{}, false
	}
	messageID := asst.Info.ID
	s.bus.Publish(event.NewMessageUpdated(sessionID, asst.Info, false))

	// NewAssistantMessage seeds a step-start part (Parts[0]); stream it before
	// any text so consumers see step-start -> text -> step-finish ordering.
	if len(asst.Parts) > 0 {
		s.bus.Publish(event.NewMessagePartUpdated(sessionID, asst.Parts[0], time.Now().UnixMilli()))
	}

	finishReason := s.runAgentLoop(ctx, sessionID, messageID, modelID, texts, callerSystem)

	// If the turn was aborted/cancelled (ctx error), the abort handler
	// (handleSessionAbort) owns the terminal session.status{idle} +
	// session.idle publish. Record the step-finish reason as "aborted"
	// (not a clean "stop") and finalize the message so it is not left
	// dangling, but do NOT emit a second session.idle here.
	aborted := ctx.Err() != nil
	reason := finishReason
	if reason == "" {
		reason = "stop"
	}
	if aborted {
		reason = "aborted"
	}

	// Append + publish the terminal step-finish part before the final
	// message.updated, matching real opencode's part ordering.
	// Compute real cost from the tokens recorded during the turn.
	var stepTokens *session.Tokens
	var stepCost float64
	if info, ok := s.store.MessageInfo(sessionID, messageID); ok {
		if info.Tokens != nil {
			stepTokens = info.Tokens
			stepCost = computeCost(info.ModelID, info.Tokens.Input, info.Tokens.Output)
		}
	}
	if stepTokens == nil {
		stepTokens = &session.Tokens{}
	}
	if sf, ok := s.store.AppendStepFinish(sessionID, messageID, reason, stepCost, stepTokens); ok {
		s.bus.Publish(event.NewMessagePartUpdated(sessionID, sf, time.Now().UnixMilli()))
	}

	// Close out open assistant text parts (set Time.End) so the completed
	// message carries both start and end on its text parts, matching real.
	s.store.FinishTextParts(sessionID, messageID)

	// Final assistant message.updated (time.completed set) -> GUARANTEED.
	s.finishGeneration(sessionID, messageID)

	if aborted {
		// handleSessionAbort already published session.status{idle} +
		// session.idle; emitting again would double the terminal event.
		return s.finalAssistantMessage(sessionID, messageID)
	}

	// Synthetic terminal session.idle -> GUARANTEED.
	s.bus.Publish(event.NewSessionStatus(sessionID, map[string]string{"type": "idle"}))
	s.bus.Publish(event.NewSessionIdle(sessionID))

	return s.finalAssistantMessage(sessionID, messageID)
}

// finalAssistantMessage returns a deep copy of the completed assistant
// MessageWithParts for the synchronous response.
func (s *Server) finalAssistantMessage(sessionID, messageID string) (session.MessageWithParts, bool) {
	return s.store.GetMessage(sessionID, messageID)
}

// emitDelta appends the delta to the store and publishes BOTH the droppable
// message.part.delta and the full-snapshot message.part.updated.
func (s *Server) emitDelta(sessionID, messageID, field, delta string) {
	part, ok := s.store.AppendTextDelta(sessionID, messageID, field, delta)
	if !ok {
		return
	}
	s.bus.Publish(event.NewMessagePartDelta(sessionID, messageID, part.ID, field, delta))
	s.bus.Publish(event.NewMessagePartUpdated(sessionID, part, time.Now().UnixMilli()))
}

// finishGeneration marks the assistant message completed and publishes the
// final message.updated (guaranteed-delivery, the canonical completion signal).
func (s *Server) finishGeneration(sessionID, messageID string) {
	info, ok := s.store.CompleteAssistantMessage(sessionID, messageID)
	if !ok {
		return
	}
	s.bus.Publish(event.NewMessageUpdated(sessionID, info, true))
	s.store.PersistSession(sessionID)
}

// joinTexts concatenates prompt text parts with newlines.
func joinTexts(texts []string) string {
	out := ""
	for i, t := range texts {
		if i > 0 {
			out += "\n"
		}
		out += t
	}
	return out
}
