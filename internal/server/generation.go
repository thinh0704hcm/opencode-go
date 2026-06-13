package server

import (
	"context"
	"strings"
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

// runGenerationSync runs the full generation pipeline (start turn, loop,
// finish) and blocks until the assistant message is completed. It returns the
// final {info, parts} for the assistant message.
func (s *Server) runGenerationSync(sessionID, parentID, providerID, modelID string, texts, images []string, system string, agent Agent) (session.MessageWithParts, bool) {
	ctx, cancel := context.WithCancel(context.Background())
	s.registerCancel(sessionID, cancel)
	defer func() {
		s.clearCancel(sessionID)
		cancel()
	}()

	return s.runGenerationSyncCtx(ctx, sessionID, parentID, providerID, modelID, texts, images, system, agent)
}

// runGenerationSyncCtx executes the core of runGenerationSync using a provided context.
func (s *Server) runGenerationSyncCtx(ctx context.Context, sessionID, parentID, providerID, modelID string, texts, images []string, system string, agent Agent) (session.MessageWithParts, bool) {
	// 1. Create the assistant message
	asst, ok := s.store.NewAssistantMessage(sessionID, parentID, providerID, modelID, agent.Name, "chat")
	if !ok {
		return session.MessageWithParts{}, false
	}

	// 2. Publish message.updated(assistant) so the TUI sees the empty bubble
	s.bus.Publish(event.NewMessageUpdated(sessionID, asst.Info, false))
	// Publish the auto-created step-start part so the TUI renders the step marker.
	if len(asst.Parts) > 0 {
		s.bus.Publish(event.NewMessagePartUpdated(sessionID, asst.Parts[0], time.Now().UnixMilli()))
	}

	// 3. Run the agent loop
	finishReason := s.runAgentLoop(ctx, sessionID, asst.Info.ID, parentID, modelID, texts, images, system, agent)

	// Record terminal reason and compute final step cost for the step-finish part.
	reason := finishReason
	if reason == "" {
		reason = "stop"
	}
	aborted := ctx.Err() != nil
	if aborted {
		reason = "aborted"
	}

	var stepTokens *session.Tokens
	var stepCost float64
	if info, ok := s.store.MessageInfo(sessionID, asst.Info.ID); ok && info.Tokens != nil {
		stepTokens = info.Tokens
		stepCost = computeCost(info.ModelID, info.Tokens.Input, info.Tokens.Output)
	}
	if stepTokens == nil {
		stepTokens = &session.Tokens{}
	}
	if sf, ok := s.store.AppendStepFinish(sessionID, asst.Info.ID, reason, stepCost, stepTokens); ok {
		s.bus.Publish(event.NewMessagePartUpdated(sessionID, sf, time.Now().UnixMilli()))
	}

	// 4. Final completion
	s.finishGeneration(sessionID, asst.Info.ID)
	return s.store.GetMessage(sessionID, asst.Info.ID)
}

// runGenerationAsync runs the generation pipeline in a background goroutine,
// publishing the full sequence of SSE events. It returns immediately.
func (s *Server) runGenerationAsync(sessionID, parentID, providerID, modelID string, texts, images []string, system string, agent Agent) {
	go func() {
		s.runGenerationSync(sessionID, parentID, providerID, modelID, texts, images, system, agent)
	}()
}

// startOrQueue adds a generation turn to the session's serial queue. If the
// queue is currently empty, it starts the turn immediately. It returns the
// admitted sequence number and true if admitted. It returns false if
// delivery=="steer" and the session is already busy.
func (s *Server) startOrQueue(sessionID, parentID, providerID, modelID string, texts, images []string, system string, agent Agent, delivery string) (int64, bool) {
	s.sesMu.Lock()
	defer s.sesMu.Unlock()

	w := s.sesQueue[sessionID]
	if w == nil {
		w = &sessionWork{
			sessionID: sessionID,
			queue:     []*generationTask{},
		}
		s.sesQueue[sessionID] = w
	}

	if w.running && delivery == "steer" {
		return 0, false
	}

	w.admitSeq++
	seq := w.admitSeq

	task := &generationTask{
		parentID:   parentID,
		providerID: providerID,
		modelID:    modelID,
		texts:      texts,
		images:     images,
		system:     system,
		agent:      agent,
	}

	w.queue = append(w.queue, task)

	if w.running {
		return seq, true
	}

	w.running = true
	go s.processQueue(w)
	return seq, true
}

func (s *Server) processQueue(w *sessionWork) {
	for {
		s.sesMu.Lock()
		if len(w.queue) == 0 || w.draining {
			w.draining = false
			w.running = false
			s.sesMu.Unlock()
			// Publish idle events so the TUI knows we're done
			s.bus.Publish(event.NewSessionStatus(w.sessionID, map[string]string{"type": "idle"}))
			s.bus.Publish(event.NewSessionIdle(w.sessionID))
			return
		}

		task := w.queue[0]
		w.queue = w.queue[1:]
		s.sesMu.Unlock()

		// Publish busy status
		s.bus.Publish(event.NewSessionStatus(w.sessionID, map[string]string{"type": "busy"}))

		s.runGenerationSync(w.sessionID, task.parentID, task.providerID, task.modelID, task.texts, task.images, task.system, task.agent)
	}
}

type generationTask struct {
	parentID   string
	providerID string
	modelID    string
	texts      []string
	images     []string
	system     string
	agent      Agent
}

type sessionWork struct {
	sessionID string
	running   bool
	draining  bool
	admitSeq  int64
	queue     []*generationTask
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
	updated := s.store.FinishOpenParts(sessionID, messageID)
	for i := range updated {
		s.bus.Publish(event.NewMessagePartUpdated(sessionID, updated[i], time.Now().UnixMilli()))
	}
	info, ok := s.store.CompleteAssistantMessage(sessionID, messageID)
	if !ok {
		return
	}
	s.bus.Publish(event.NewMessageUpdated(sessionID, info, true))
	s.store.PersistSession(sessionID)
}

func firstLine(text string, maxLen int) string {
	lines := strings.Split(text, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if len(l) > maxLen {
			return l[:maxLen] + "…"
		}
		return l
	}
	return ""
}
