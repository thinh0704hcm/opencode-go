package server

import (
	"encoding/json"
	"net/http"
	"time"
	"strings"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/tool"
)

// shellModel is the optional model selector in the shell body.
type shellModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// shellRequest is the POST /session/{id}/shell body. The opencode TUI's
// integrated shell posts this (not the /pty/* endpoints): a single command,
// an optional agent name, and an optional model selector.
type shellRequest struct {
	Command string      `json:"command"`
	Agent   string      `json:"agent"`
	Model   *shellModel `json:"model"`
}

// handleSessionShell serves POST /session/{id}/shell. It runs the command via
// the bash tool inside the workspace sandbox, recording it as a normal
// assistant turn (user message -> assistant message with a bash tool part) and
// emitting the same SSE event sequence as a regular prompt so the TUI renders
// it identically. Blocks until the command completes, then returns the final
// assistant {info, parts}. 404 if the session is unknown.
func (s *Server) handleSessionShell(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if _, ok := s.store.GetSession(id); !ok {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }

    // Validate JSON content type and required fields.
    if !requireJSON(w, r) {
        return
    }
    var req shellRequest
    if !decodeStrictBody(w, r, &req, false) {
        return
    }
    if strings.TrimSpace(req.Command) == "" || strings.TrimSpace(req.Agent) == "" {
        writeJSON(w, http.StatusBadRequest, map[string]any{"error": "command and agent must be non-empty"})
        return
    }


	var providerID, modelID string
	if req.Model != nil {
		providerID = req.Model.ProviderID
		modelID = req.Model.ModelID
	}

	// 1. User message carrying the command, published before the assistant turn.
	userMsg, ok := s.store.AppendUserMessage(id, "", providerID, modelID, "build", []string{req.Command})
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if updated, ok := s.store.GetSession(id); ok {
		s.bus.Publish(event.NewSessionUpdated(id, updated))
	}
	s.publishUserMessage(id, userMsg)

	// 2. Run synchronously (block), mirroring runGenerationSync's event order.
	s.bus.Publish(event.NewSessionStatus(id, map[string]string{"type": "busy"}))

	asst, ok := s.store.NewAssistantMessage(id, userMsg.Info.ID, providerID, modelID, "build", "build")
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	messageID := asst.Info.ID
	s.bus.Publish(event.NewMessageUpdated(id, asst.Info, false))

	// Stream the seeded step-start part (Parts[0]) before any tool activity.
	if len(asst.Parts) > 0 {
		s.bus.Publish(event.NewMessagePartUpdated(id, asst.Parts[0], time.Now().UnixMilli()))
	}

	sb, err := tool.New(s.workdir)
	if err != nil {
		s.bus.Publish(event.NewSessionError(id, map[string]string{"message": err.Error()}))
		s.bus.Publish(event.NewSessionStatus(id, map[string]string{"type": "idle"}))
		s.bus.Publish(event.NewSessionIdle(id))
		writeError(w, http.StatusInternalServerError, "failed to create sandbox")
		return
	}

	// Running tool part.
	toolInput := map[string]any{"command": req.Command}
	if p, ok := s.store.AppendToolPart(id, messageID, "bash", "shell_1", "running", toolInput, ""); ok {
		s.bus.Publish(event.NewMessagePartUpdated(id, p, time.Now().UnixMilli()))
	}

	// Execute the command via the bash tool.
	input, _ := json.Marshal(map[string]string{"command": req.Command})
	out, isErr := executeToolCall(r.Context(), s.tools, sb, provider.ToolCall{
		ID:    "shell_1",
		Name:  "bash",
		Input: json.RawMessage(input),
	})

	status := "completed"
	if isErr {
		status = "error"
	}
	if p2, ok := s.store.AppendToolPart(id, messageID, "bash", "shell_1", status, toolInput, out); ok {
		s.bus.Publish(event.NewMessagePartUpdated(id, p2, time.Now().UnixMilli()))
	}

	// Complete the assistant message and emit the guaranteed terminal events.
	if info, ok := s.store.CompleteAssistantMessage(id, messageID); ok {
		s.bus.Publish(event.NewMessageUpdated(id, info, true))
		s.store.PersistSession(id)
	}
	s.bus.Publish(event.NewSessionStatus(id, map[string]string{"type": "idle"}))
	s.bus.Publish(event.NewSessionIdle(id))

	// 3. Return the final assistant {info, parts}.
	final, ok := s.store.GetMessage(id, messageID)
	if !ok {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	writeJSON(w, http.StatusOK, final)
}
