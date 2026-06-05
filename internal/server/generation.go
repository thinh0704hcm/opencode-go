package server

import (
	"context"
	"time"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/session"
)

// publishUserMessage publishes message.updated for the user message.
func (s *Server) publishUserMessage(sessionID string, info any) {
	s.bus.Publish(event.NewMessageUpdated(sessionID, info, false))
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
func (s *Server) runGeneration(sessionID, modelID string, texts []string) {
	s.runGenerationSync(sessionID, modelID, texts)
}

// runGenerationSync runs the assistant turn inline (same pipeline and event
// sequence as runGeneration) and returns the final assistant MessageWithParts
// once the turn has completed. ok is false if the session/message could not be
// resolved. The async path wraps this in a goroutine; the synchronous
// POST /session/{id}/message handler blocks on it directly.
func (s *Server) runGenerationSync(sessionID, modelID string, texts []string) (session.MessageWithParts, bool) {
	ctx := context.Background()

	// session.status{type:"busy"}
	s.bus.Publish(event.NewSessionStatus(sessionID, map[string]string{"type": "busy"}))

	// Assistant message (time.completed=null) + message.updated(assistant).
	asst, ok := s.store.NewAssistantMessage(sessionID)
	if !ok {
		s.bus.Publish(event.NewSessionError(sessionID, map[string]string{"message": "session not found"}))
		s.bus.Publish(event.NewSessionIdle(sessionID))
		return session.MessageWithParts{}, false
	}
	messageID := asst.Info.ID
	s.bus.Publish(event.NewMessageUpdated(sessionID, asst.Info, false))

	s.runAgentLoop(ctx, sessionID, messageID, modelID, texts)

	// Final assistant message.updated (time.completed set) -> GUARANTEED.
	s.finishGeneration(sessionID, messageID)

	// Synthetic terminal session.idle -> GUARANTEED.
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
