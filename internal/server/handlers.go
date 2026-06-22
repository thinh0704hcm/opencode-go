package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/session"
)

// healthResponse is the /global/health body.
type healthResponse struct {
	Healthy bool   `json:"healthy"`
	Version string `json:"version"`
}

// handleHealth serves GET /global/health and GET /api/global/health.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Healthy: true, Version: Version})
}

// handleAuthSet serves PUT /auth/{id} and PUT /auth/{providerID}.
func (s *Server) handleAuthSet(w http.ResponseWriter, r *http.Request) {
	s.handleTUIOK(w, r)
}

// handleAuthRemove serves DELETE /auth/{providerID}.
func (s *Server) handleAuthRemove(w http.ResponseWriter, r *http.Request) {
	s.handleTUIOK(w, r)
}

// sessionCreateRequest is the POST /session body (all optional).
type sessionCreateRequest struct {
	ParentID string `json:"parentID"`
	Title    string `json:"title"`
}

// handleSessionCreate serves POST /session, accepting an empty or partial body.
func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
    // Read raw request body (limit 1 MiB).
    r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
    data, err := io.ReadAll(r.Body)
    if err != nil {
        writeError(w, http.StatusBadRequest, "failed to read request body")
        return
    }

    var req sessionCreateRequest
    if len(data) == 0 {
        // empty body accepted
    } else {
        if !requireJSON(w, r) {
            return
        }
        var raw map[string]any
        if err := json.Unmarshal(data, &raw); err != nil {
            writeError(w, http.StatusBadRequest, "invalid JSON")
            return
        }
        if v, ok := raw["title"]; ok {
            if s, ok2 := v.(string); !ok2 || strings.TrimSpace(s) == "" {
                writeError(w, http.StatusBadRequest, "invalid title")
                return
            } else {
                req.Title = s
            }
        }
        if v, ok := raw["parentID"]; ok {
            if s, ok2 := v.(string); ok2 {
                req.ParentID = s
            } else {
                writeError(w, http.StatusBadRequest, "invalid parentID")
                return
            }
        }
    }

    s.bus.ClearReplay()

    dir := directoryParam(r)
    sess := s.store.CreateSession(req.ParentID, req.Title, dir)
    s.store.PersistSession(sess.ID)
    s.bus.Publish(event.NewSessionCreated(sess.ID, sess))
    writeJSON(w, http.StatusOK, sess)
}

// promptPart is one part of a prompt body.
type promptPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Mime string `json:"mime,omitempty"`
	URL  string `json:"url,omitempty"`
}

// promptModel is the model selector in the prompt body.
type promptModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// promptAsyncRequest is the POST /session/{id}/prompt_async body.
type agentSelector string

func (a *agentSelector) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*a = agentSelector(s)
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	for _, key := range []string{"name", "id", "agent"} {
		if v, ok := obj[key].(string); ok {
			*a = agentSelector(v)
			return nil
		}
	}
	return nil
}

func (a agentSelector) String() string { return string(a) }

type promptAsyncRequest struct {
	MessageID string        `json:"messageID"`
	Model     promptModel   `json:"model"`
	Agent     agentSelector `json:"agent"`
	Mode      agentSelector `json:"mode"`
	AgentID   agentSelector `json:"agentID"`
	Parts     []promptPart  `json:"parts"`
	System    string        `json:"system,omitempty"`
}

func (r promptAsyncRequest) AgentName() string {
	// `mode` is often the opencode runtime mode ("build"), not the selected
	// agent. Prefer explicit agent fields and only use mode when non-default.
	for _, v := range []agentSelector{r.Agent, r.AgentID} {
		if strings.TrimSpace(v.String()) != "" {
			return v.String()
		}
	}
	m := strings.TrimSpace(r.Mode.String())
	if m != "" && m != "build" {
		return m
	}
	return ""
}

// handlePromptAsync serves POST /session/{id}/prompt_async. Returns 204
// immediately and runs generation in a background goroutine (architecture §2.4).
func (s *Server) handlePromptAsync(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var req promptAsyncRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	texts := make([]string, 0, len(req.Parts))
	images := make([]string, 0, len(req.Parts))
	for _, p := range req.Parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		} else if p.Type == "file" && strings.HasPrefix(p.Mime, "image/") && p.URL != "" {
			images = append(images, p.URL)
		}
	}

	// Validate: at least one non-empty text part required.
	hasText := false
	for _, t := range texts {
		if strings.TrimSpace(t) != "" {
			hasText = true
			break
		}
	}
	if !hasText {
		writeError(w, http.StatusBadRequest, "message content must contain at least one non-empty text part")
		return
	}

	modelID := req.Model.ModelID
	if modelID == "" {
		modelID = s.model
	}

	agent, _ := resolveAgent(s.workdir, req.AgentName())
	if agent.Name == "" {
		agent.Name = "build"
	}

	reqProviderID := req.Model.ProviderID
	if reqProviderID == "" {
		reqProviderID = s.configuredProviderID
	}
	reqModelID := req.Model.ModelID
	if reqModelID == "" {
		reqModelID = modelID
	}

	// Append the user message and publish message.updated(user) synchronously
	// so it is ordered before the assistant turn.
	userMsg, ok := s.store.AppendUserMessage(id, req.MessageID, reqProviderID, reqModelID, agent.Name, texts)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if updated, ok := s.store.GetSession(id); ok {
		s.bus.Publish(event.NewSessionUpdated(id, updated))
	}
	s.publishUserMessage(id, userMsg)

	_, ok = s.startOrQueue(id, userMsg.Info.ID, "", reqProviderID, reqModelID, texts, images, req.System, agent, "")
	if !ok {
		s.store.RemoveMessage(id, userMsg.Info.ID)
		writeJSON(w, http.StatusConflict, map[string]any{"_tag": "ConflictError", "message": "session is busy", "resource": "session"})
		return
	}
	s.bus.Publish(event.NewSessionNextPrompted(id, userMsg.Info.ID, strings.Join(texts, "\n"), "queue"))
	s.bus.Publish(event.NewSessionNextPromptAdmitted(id, userMsg.Info.ID, strings.Join(texts, "\n"), "queue"))

	w.WriteHeader(http.StatusNoContent)
}

// handlePrompt serves POST /session/{id}/message. It runs the SAME generation
// pipeline as prompt_async (emitting the identical SSE event sequence) but
// BLOCKS until the assistant turn completes, then returns 200 with the final
// assistant {info, parts}. 404 if the session is unknown.
func (s *Server) handlePrompt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.GetSession(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var req promptAsyncRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	texts := make([]string, 0, len(req.Parts))
	images := make([]string, 0, len(req.Parts))
	for _, p := range req.Parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		} else if p.Type == "file" && strings.HasPrefix(p.Mime, "image/") && p.URL != "" {
			images = append(images, p.URL)
		}
	}

	// Validate: at least one non-empty text part required.
	hasText := false
	for _, t := range texts {
		if strings.TrimSpace(t) != "" {
			hasText = true
			break
		}
	}
	if !hasText {
		writeError(w, http.StatusBadRequest, "message content must contain at least one non-empty text part")
		return
	}

	modelID := req.Model.ModelID
	if modelID == "" {
		modelID = s.model
	}

	agent, _ := resolveAgent(s.workdir, req.AgentName())
	if agent.Name == "" {
		agent.Name = "build"
	}

	reqProviderID := req.Model.ProviderID
	if reqProviderID == "" {
		reqProviderID = s.configuredProviderID
	}
	reqModelID := req.Model.ModelID
	if reqModelID == "" {
		reqModelID = modelID
	}

	// Append the user message and publish message.updated(user) synchronously
	// so it is ordered before the assistant turn.
	userMsg, ok := s.store.AppendUserMessage(id, req.MessageID, reqProviderID, reqModelID, agent.Name, texts)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if updated, ok := s.store.GetSession(id); ok {
		s.bus.Publish(event.NewSessionUpdated(id, updated))
	}
	s.publishUserMessage(id, userMsg)
	s.bus.Publish(event.NewSessionNextPrompted(id, userMsg.Info.ID, strings.Join(texts, "\n"), "queue"))
	s.bus.Publish(event.NewSessionNextPromptAdmitted(id, userMsg.Info.ID, strings.Join(texts, "\n"), "queue"))

	ctx, cancel := context.WithCancel(context.Background())
	s.registerCancel(id, cancel)
	defer func() { s.clearCancel(id); cancel() }()

	asst, ok := s.runGenerationSyncCtx(ctx, id, userMsg.Info.ID, "", reqProviderID, modelID, texts, images, req.System, agent)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	s.bus.Publish(event.NewSessionStatus(id, map[string]string{"type": "idle"}))
	s.bus.Publish(event.NewSessionIdle(id))
	writeJSON(w, http.StatusOK, asst)
}

// handleGetMessages serves GET /session/{id}/message, returning a deep-copied
// JSON array of {info, parts}.
func (s *Server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	msgs, ok := s.store.Messages(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if msgs == nil {
		msgs = []session.MessageWithParts{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

// decodeBody decodes the JSON request body into v. An empty body is treated as
// an empty object (no error).

// hasJSONContentType checks if the request has a JSON content type.
func hasJSONContentType(r *http.Request) bool {
    ct := r.Header.Get("Content-Type")
    return strings.HasPrefix(ct, "application/json")
}

// requireJSON writes a 400 error if the request doesn't have a JSON content type.
// Returns true if the request is valid (JSON), false if it was rejected.
func requireJSON(w http.ResponseWriter, r *http.Request) bool {
    if !hasJSONContentType(r) {
        writeJSON(w, http.StatusBadRequest, map[string]any{"error": "expected application/json content type"})
        return false
    }
    return true
}

// decodeStrictBody decodes a JSON request body and rejects trailing data.
// If allowEmpty is true, an empty body is accepted (no decode attempted).
// Returns true if decoding succeeded, false if an error was written.
func decodeStrictBody(w http.ResponseWriter, r *http.Request, v any, allowEmpty bool) bool {
    // Check for empty body
    if r.Body == nil {
        if allowEmpty {
            return true
        }
        writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request body is empty"})
        return false
    }

    // Peek at first byte to detect empty body
    buf, err := io.ReadAll(io.LimitReader(r.Body, 1))
    if err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]any{"error": "failed to read request body"})
        return false
    }

    if len(buf) == 0 {
        if allowEmpty {
            return true
        }
        writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request body is empty"})
        return false
    }

    // Reconstruct the reader with the peeked byte
    body := io.MultiReader(bytes.NewReader(buf), r.Body)

    // Decode JSON
    dec := json.NewDecoder(body)
    if err := dec.Decode(v); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON: " + err.Error()})
        return false
    }

    // Reject trailing data
    var trailing json.RawMessage
    if err := dec.Decode(&trailing); err != io.EOF {
        writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unexpected trailing data in request body"})
        return false
    }

    return true
}

// decodeBody decodes the JSON request body into v. An empty body is treated as
// an empty object (no error).
func decodeBody(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		// EOF (empty body) is acceptable for endpoints that accept {}.
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}
