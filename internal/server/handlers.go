package server

import (
	"encoding/json"
	"net/http"

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

// sessionCreateRequest is the POST /session body (all optional).
type sessionCreateRequest struct {
	ParentID string `json:"parentID"`
	Title    string `json:"title"`
}

// handleSessionCreate serves POST /session, accepting an empty or partial body.
func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	var req sessionCreateRequest
	_ = decodeBody(r, &req) // body {} accepted; ignore decode errors

	dir := directoryOf(r)
	sess := s.store.CreateSession(req.ParentID, req.Title, dir)
	writeJSON(w, http.StatusOK, sess)
}

// promptPart is one part of a prompt body.
type promptPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// promptModel is the model selector in the prompt body.
type promptModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// promptAsyncRequest is the POST /session/{id}/prompt_async body.
type promptAsyncRequest struct {
	MessageID string       `json:"messageID"`
	Model     promptModel  `json:"model"`
	Agent     string       `json:"agent"`
	Parts     []promptPart `json:"parts"`
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
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	texts := make([]string, 0, len(req.Parts))
	for _, p := range req.Parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}

	modelID := req.Model.ModelID
	if modelID == "" {
		modelID = s.model
	}

	// Append the user message and publish message.updated(user) synchronously
	// so it is ordered before the assistant turn.
	userMsg, ok := s.store.AppendUserMessage(id, req.MessageID, req.Model.ProviderID, req.Model.ModelID, texts)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	s.publishUserMessage(id, userMsg)

	// Run the generation in the background; return 204 immediately.
	go s.runGeneration(id, userMsg.Info.ID, req.Model.ProviderID, modelID, texts)

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
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	texts := make([]string, 0, len(req.Parts))
	for _, p := range req.Parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}

	modelID := req.Model.ModelID
	if modelID == "" {
		modelID = s.model
	}

	// Append the user message and publish message.updated(user) synchronously
	// so it is ordered before the assistant turn.
	userMsg, ok := s.store.AppendUserMessage(id, req.MessageID, req.Model.ProviderID, req.Model.ModelID, texts)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	s.publishUserMessage(id, userMsg)

	// Block until the assistant turn completes, reusing the shared pipeline.
	asst, ok := s.runGenerationSync(id, userMsg.Info.ID, req.Model.ProviderID, modelID, texts)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
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
func decodeBody(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		// EOF (empty body) is acceptable for endpoints that accept {}.
		if err.Error() == "EOF" {
			return nil
		}
		return err
	}
	return nil
}
