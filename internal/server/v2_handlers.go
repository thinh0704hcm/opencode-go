package server

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/opencode-go/opencode-go/internal/config"
	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/session"
)

var (
	sessionNotFoundError = map[string]any{
		"_tag":     "NotFoundError",
		"message":  "session not found",
		"resource": "session",
	}
)

func (s *Server) handleV2Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"healthy": true})
}

func (s *Server) handleV2Location(w http.ResponseWriter, r *http.Request) {
	dir := directoryParam(r)
	if dir == "" {
		dir = s.workdir
	}
	id := filepath.Base(dir)
	if dir == "" || id == "." || id == string(filepath.Separator) {
		id = "global"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"directory": dir,
		"project":   map[string]any{"id": id, "directory": dir},
	})
}

type sessionV2Info struct {
	ID        string `json:"id"`
	ParentID  string `json:"parentID,omitempty"`
	ProjectID string `json:"projectID"`
	Agent     string `json:"agent,omitempty"`
	Model     *struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
	} `json:"model,omitempty"`
	Cost   float64 `json:"cost"`
	Tokens struct {
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
	Time struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
	Title    string `json:"title"`
	Location struct {
		Directory string `json:"directory"`
	} `json:"location"`
}

func parseOffsetCursor(raw string, total int) int {
	offset := 0
	if raw != "" {
		if b, err := base64.StdEncoding.DecodeString(raw); err == nil {
			if n, err := strconv.Atoi(string(b)); err == nil {
				offset = n
			}
		}
	}
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	return offset
}

func parsePageLimit(r *http.Request) int {
	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}
	return limit
}

func (s *Server) handleV2SessionList(w http.ResponseWriter, r *http.Request) {
	limit := parsePageLimit(r)
	sessions := s.store.List()

	order := r.URL.Query().Get("order")
	if order == "" {
		order = "desc"
	}
	if order == "desc" {
		for i, j := 0, len(sessions)-1; i < j; i, j = i+1, j-1 {
			sessions[i], sessions[j] = sessions[j], sessions[i]
		}
	}

	total := len(sessions)
	offset := parseOffsetCursor(r.URL.Query().Get("cursor"), total)
	end := offset + limit
	if end > total {
		end = total
	}
	page := sessions[offset:end]

	var nextCursor, prevCursor any
	if end < total {
		nextCursor = base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(end)))
	}
	if offset > 0 {
		prev := offset - limit
		if prev < 0 {
			prev = 0
		}
		prevCursor = base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(prev)))
	}

	data := make([]sessionV2Info, 0, len(page))
	for _, sess := range page {
		data = append(data, s.mapToV2Info(sess))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":   data,
		"cursor": map[string]any{"previous": prevCursor, "next": nextCursor},
	})
}

func (s *Server) handleV2SessionGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	sess, ok := s.store.GetSession(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.mapToV2Info(sess)})
}

func (s *Server) mapToV2Info(sess session.Session) sessionV2Info {
	projID := filepath.Base(sess.Directory)
	if projID == "." || projID == "" || projID == string(filepath.Separator) {
		projID = "global"
	}
	info := sessionV2Info{
		ID:        sess.ID,
		ParentID:  sess.ParentID,
		ProjectID: projID,
		Title:     sess.Title,
	}
	info.Time.Created = sess.Time.Created
	info.Time.Updated = sess.Time.Updated
	info.Location.Directory = sess.Directory

	if msgs, ok := s.store.Messages(sess.ID); ok {
		for _, m := range msgs {
			if m.Info.Cost != nil {
				info.Cost += *m.Info.Cost
			}
			if m.Info.Tokens != nil {
				info.Tokens.Input += m.Info.Tokens.Input
				info.Tokens.Output += m.Info.Tokens.Output
				info.Tokens.Reasoning += m.Info.Tokens.Reasoning
				info.Tokens.Cache.Read += m.Info.Tokens.Cache.Read
				info.Tokens.Cache.Write += m.Info.Tokens.Cache.Write
			}
			if m.Info.Role == "assistant" && m.Info.ModelID != "" {
				info.Agent = m.Info.Agent
				info.Model = &struct {
					ID         string `json:"id"`
					ProviderID string `json:"providerID"`
				}{
					ID:         m.Info.ProviderID + "/" + m.Info.ModelID,
					ProviderID: m.Info.ProviderID,
				}
			}
		}
	}
	return info
}

type v2CreateSessionRequest struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Location struct {
		Directory string `json:"directory"`
	} `json:"location"`
}

func (s *Server) handleV2SessionCreate(w http.ResponseWriter, r *http.Request) {
	var req v2CreateSessionRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ID != "" {
		if _, exists := s.store.GetSession(req.ID); exists {
			writeJSON(w, http.StatusConflict, map[string]any{
				"_tag":     "ConflictError",
				"message":  "session already exists",
				"resource": "session",
			})
			return
		}
	}
	dir := req.Location.Directory
	if dir == "" {
		dir = s.workdir
	}
	sess := s.store.CreateSessionWithID(req.ID, "", req.Title, dir)
	s.store.PersistSession(sess.ID)
	writeJSON(w, http.StatusOK, map[string]any{"data": s.mapToV2Info(sess)})
}

type v2PromptRequest struct {
	ID     string `json:"id"`
	Prompt struct {
		Text  string `json:"text"`
		Files []struct {
			URI  string `json:"uri"`
			Mime string `json:"mime"`
		} `json:"files"`
	} `json:"prompt"`
	Model *struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	} `json:"model"`
	Agent    string `json:"agent"`
	Delivery string `json:"delivery"` // "steer" | "queue"; default "steer"
	Resume   bool   `json:"resume"`
}

type sessionInputAdmitted struct {
	AdmittedSeq uint64 `json:"admittedSeq"`
	ID          string `json:"id"`
	SessionID   string `json:"sessionID"`
	Prompt      struct {
		Text string `json:"text"`
	} `json:"prompt"`
	Delivery    string `json:"delivery"`
	TimeCreated int64  `json:"timeCreated"`
}

func (s *Server) handleV2SessionPrompt(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, ok := s.store.GetSession(sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var req v2PromptRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	delivery := req.Delivery
	if delivery == "" {
		delivery = "steer"
	}

	msgID := req.ID
	if msgID == "" {
		msgID = event.NewID("msg")
	}

	// Extract images from files with mime "image/*"
	var texts []string
	var images []string
	if req.Prompt.Text != "" {
		texts = append(texts, req.Prompt.Text)
	}
	for _, f := range req.Prompt.Files {
		if strings.HasPrefix(f.Mime, "image/") {
			images = append(images, f.URI)
		}
	}

	// Resolve agent
	agent, _ := resolveAgent(s.workdir, req.Agent)
	if agent.Name == "" {
		agent.Name = "build"
	}

	providerID := s.provider.ID()
	modelID := s.model
	if req.Model != nil && req.Model.ModelID != "" {
		providerID = req.Model.ProviderID
		modelID = req.Model.ModelID
	}

	var resumeID string
	var targetParentID = msgID

	if req.Resume {
		msgs, ok := s.store.Messages(sessionID)
		if !ok {
			// A newly created session without messages still returns ok=true if session exists
			// so if ok=false the session really does not exist.
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Info.Role == "assistant" {
				resumeID = msgs[i].Info.ID
				break
			}
		}
		if resumeID == "" {
			writeError(w, http.StatusBadRequest, "no assistant message to resume")
			return
		}

		if len(texts) == 0 && len(images) == 0 {
			// Pure resume: do not create an empty user message.
			targetParentID = "" // ignored when resuming
		} else {
			// Resume with a prompt: append user message as usual, then resume.
			// (Documented behavior: treated as normal prompt + resume flag)
			msg, ok := s.store.AppendUserMessage(sessionID, msgID, providerID, modelID, agent.Name, texts)
			if !ok {
				writeError(w, http.StatusNotFound, "session not found")
				return
			}
			if updated, ok := s.store.GetSession(sessionID); ok {
				s.bus.Publish(event.NewSessionUpdated(sessionID, updated))
			}
			s.publishUserMessage(sessionID, msg)
		}
	} else {
		// Normal generation request
		msg, ok := s.store.AppendUserMessage(sessionID, msgID, providerID, modelID, agent.Name, texts)
		if !ok {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if updated, ok := s.store.GetSession(sessionID); ok {
			s.bus.Publish(event.NewSessionUpdated(sessionID, updated))
		}
		s.publishUserMessage(sessionID, msg)
	}

	seq, ok := s.startOrQueue(sessionID, targetParentID, resumeID, providerID, modelID, texts, images, "", agent, delivery, nil)
	if !ok {
		s.store.RemoveMessage(sessionID, msgID)
		writeJSON(w, http.StatusConflict, map[string]any{"_tag": "ConflictError", "message": "session is busy", "resource": "session"})
		return
	}

	s.bus.Publish(event.NewSessionNextPrompted(sessionID, msgID, req.Prompt.Text, delivery))
	s.bus.Publish(event.NewSessionNextPromptAdmitted(sessionID, msgID, req.Prompt.Text, delivery))

	resp := sessionInputAdmitted{
		AdmittedSeq: seq,
		ID:          msgID,
		SessionID:   sessionID,
		Delivery:    delivery,
		TimeCreated: time.Now().UnixMilli(),
	}
	resp.Prompt.Text = req.Prompt.Text
	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func (s *Server) handleV2SessionWait(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, ok := s.store.GetSession(sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Subscribe BEFORE reading work.running to avoid missing the idle event
	// that fires between the lock-release and the channel read.
	sub, cancel := s.bus.SubscribeFiltered(func(ev event.Event) bool {
		return ev.Type == event.TypeSessionIdle && eventSessionID(ev) == sessionID
	})
	defer cancel()

	s.sesMu.Lock()
	work := s.sesQueue[sessionID]
	idle := work == nil || !work.running
	s.sesMu.Unlock()

	if idle {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case _, ok := <-sub.Events():
			if !ok {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		case <-ticker.C:
			w.WriteHeader(http.StatusNoContent)
			return
		case <-r.Context().Done():
			return
		}
	}
}

func eventSessionID(ev event.Event) string {
	switch p := ev.Properties.(type) {
	case event.SessionIdleProps:
		return p.SessionID
	case event.SessionStatusProps:
		return p.SessionID
	case event.SessionUpdatedProps:
		return p.SessionID
	case event.MessageUpdatedProps:
		return p.SessionID
	case event.PartDeltaProps:
		return p.SessionID
	case event.PartUpdatedProps:
		return p.SessionID
	case event.SessionErrorProps:
		return p.SessionID
	case event.SessionDeletedProps:
		return p.SessionID
	case event.PermissionRepliedProps:
		return p.SessionID
	case map[string]any:
		if sid, ok := p["sessionID"].(string); ok {
			return sid
		}
	case event.SessionNextPromptProps:
		return p.SessionID
	case event.SessionNextStepStartedProps:
		return p.SessionID
	case event.SessionNextStepEndedProps:
		return p.SessionID
	case event.SessionNextStepFailedProps:
		return p.SessionID
	case event.SessionNextReasoningStartedProps:
		return p.SessionID
	case event.SessionNextReasoningDeltaProps:
		return p.SessionID
	case event.SessionNextReasoningEndedProps:
		return p.SessionID
	case event.SessionNextTextStartedProps:
		return p.SessionID
	case event.SessionNextTextDeltaProps:
		return p.SessionID
	case event.SessionNextTextEndedProps:
		return p.SessionID
	case event.SessionNextToolInputStartedProps:
		return p.SessionID
	case event.SessionNextToolInputDeltaProps:
		return p.SessionID
	case event.SessionNextToolInputEndedProps:
		return p.SessionID
	case event.SessionNextToolCalledProps:
		return p.SessionID
	case event.SessionNextToolSuccessProps:
		return p.SessionID
	case event.SessionNextToolFailedProps:
		return p.SessionID
	case event.SessionNextRetriedProps:
		return p.SessionID
	}
	return ""
}

func (s *Server) handleV2SessionMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	msgs, ok := s.store.Messages(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	limit := parsePageLimit(r)
	order := r.URL.Query().Get("order") // "asc" | "desc", default "asc"

	total := len(msgs)
	offset := parseOffsetCursor(r.URL.Query().Get("cursor"), total)
	end := offset + limit
	if end > total {
		end = total
	}

	data := make([]any, 0, end-offset)
	if order == "desc" {
		start := total - end
		if start < 0 {
			start = 0
		}
		for i := total - offset - 1; i >= start; i-- {
			data = append(data, s.mapToV2Message(msgs[i]))
		}
	} else {
		for i := offset; i < end; i++ {
			data = append(data, s.mapToV2Message(msgs[i]))
		}
	}

	var nextCursor, prevCursor any
	if end < total {
		nextCursor = base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(end)))
	}
	if offset > 0 {
		prev := offset - limit
		if prev < 0 {
			prev = 0
		}
		prevCursor = base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(prev)))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":   data,
		"cursor": map[string]any{"previous": prevCursor, "next": nextCursor},
	})
}

func (s *Server) mapToV2Message(m session.MessageWithParts) any {
	if m.Info.Role == "user" {
		providerID := ""
		modelID := ""
		if m.Info.Model != nil {
			providerID = m.Info.Model.ProviderID
			modelID = m.Info.Model.ModelID
		}
			return map[string]any{
				"id":        m.Info.ID,
				"sessionID": m.Info.SessionID,
				"type":      "user",
				"time":      map[string]any{"created": m.Info.Time.Created},
				"text":      partsText(m.Parts, "text"),
				"agent":     m.Info.Agent,
				"metadata":  map[string]any{},
				"model": map[string]any{
				"id":         providerID + "/" + modelID,
				"providerID": providerID,
				"modelID":    modelID,
			},
		}
	}

	var content []any
	for _, p := range m.Parts {
		switch p.Type {
		case "text":
			tp := map[string]any{
				"type": "text",
				"id":   p.ID,
				"text": p.Text,
			}
			if p.Time != nil {
				var endMS any
				if p.Time.End != nil {
					endMS = *p.Time.End
				}
				tp["time"] = map[string]any{"start": p.Time.Start, "end": endMS}
			}
			content = append(content, tp)
		case "reasoning":
			if p.Text == "" {
				continue
			}
			rp := map[string]any{
				"type": "reasoning",
				"id":   p.ID,
				"text": p.Text,
			}
			if p.Time != nil {
				var endMS any
				if p.Time.End != nil {
					endMS = *p.Time.End
				}
				rp["time"] = map[string]any{"start": p.Time.Start, "end": endMS}
			}
			content = append(content, rp)
		case "subtask":
			sp := map[string]any{
				"type":        "subtask",
				"id":          p.ID,
				"prompt":      p.Prompt,
				"description": p.Description,
				"agent":       p.Agent,
			}
			if p.Model != nil {
				sp["model"] = map[string]any{
					"providerID": p.Model.ProviderID,
					"modelID":    p.Model.ModelID,
				}
			}
			if p.TargetSessionID != "" {
				sp["sessionID"] = p.TargetSessionID
			}
			content = append(content, sp)
		case "tool":
			if p.State == nil {
				continue
			}
			if p.Tool == "delegate" || p.Tool == "task" {
				if p.State.Status != "error" {
					continue
				}
			}
			state := map[string]any{
				"status":   p.State.Status,
				"input":    p.State.Input,
				"title":    p.State.Title,
				"metadata": p.State.Metadata,
			}
			if p.State.Status == "error" {
				state["error"] = p.State.Error
			} else {
				state["output"] = p.State.Output
			}
			if p.State.Time != nil {
				endVal := p.State.Time.End
				var endMS any
				if endVal != nil {
					endMS = *endVal
				}
				state["time"] = map[string]any{
					"start": p.State.Time.Start,
					"end":   endMS,
				}
			}
			content = append(content, map[string]any{
				"type":     "tool",
				"id":       p.ID,
				"callID":   p.CallID,
				"tool":     p.Tool,
				"sessionID": p.SessionID,
				"messageID": p.MessageID,
				"state":    state,
			})
		case "step-start":
			content = append(content, map[string]any{
				"type":      "step-start",
				"id":        p.ID,
				"sessionID": p.SessionID,
				"messageID": p.MessageID,
			})
		case "step-finish":
			sf := map[string]any{
				"type":      "step-finish",
				"id":        p.ID,
				"sessionID": p.SessionID,
				"messageID": p.MessageID,
				"reason":    p.Reason,
				"cost":      p.Cost,
			}
			if p.Tokens != nil {
				sf["tokens"] = p.Tokens
			}
			content = append(content, sf)
		}
	}

	asst := map[string]any{
		"id":         m.Info.ID,
		"sessionID":  m.Info.SessionID,
		"type":       "assistant",
		"parentID":   m.Info.ParentID,
		"providerID": m.Info.ProviderID,
		"modelID":    m.Info.ModelID,
		"agent":      m.Info.Agent,
		"time":       map[string]any{"created": m.Info.Time.Created, "completed": m.Info.Time.Completed},
		"model": map[string]any{
			"id":         m.Info.ProviderID + "/" + m.Info.ModelID,
			"providerID": m.Info.ProviderID,
			"modelID":    m.Info.ModelID,
		},
		"content": content,
		"finish":  m.Info.Finish,
		"cost":    m.Info.Cost,
	}
	if m.Info.Tokens != nil {
		asst["tokens"] = m.Info.Tokens
	}
	if asst["type"] == nil || asst["type"] == "" {
		asst["type"] = "assistant"
	}
	return asst
}

type agentV2Info struct {
	ID          string `json:"id"`
	Mode        string `json:"mode"`
	Description string `json:"description,omitempty"`
	System      string `json:"system,omitempty"`
	Permissions []any  `json:"permissions"`
	Request     struct {
		Headers map[string]string `json:"headers"`
		Body    map[string]any    `json:"body"`
	} `json:"request"`
}

func (s *Server) handleV2AgentList(w http.ResponseWriter, r *http.Request) {
	agents := loadAgents(s.workdir)
	var data []agentV2Info
	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		a := agents[name]
		info := agentV2Info{
			ID:          a.Name,
			Mode:        a.Mode,
			Description: a.Description,
			System:      a.Prompt,
			Permissions: []any{},
		}
		info.Request.Headers = map[string]string{}
		info.Request.Body = map[string]any{}
		data = append(data, info)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"location": map[string]any{"directory": s.workdir},
		"data":     data,
	})
}

type modelV2Info struct {
	ID           string `json:"id"`
	ProviderID   string `json:"providerID"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	Capabilities struct {
		Tools  bool     `json:"tools"`
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"capabilities"`
	Limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
	Cost []map[string]any `json:"cost"`
	API  struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Package string `json:"package"`
		URL     string `json:"url,omitempty"`
	} `json:"api"`
	Request struct {
		Headers map[string]string `json:"headers"`
		Body    map[string]any    `json:"body"`
	} `json:"request"`
	Variants []any `json:"variants"`
	Time     struct {
		Released int64 `json:"released"`
	} `json:"time"`
	Status string `json:"status"`
}

func (s *Server) handleV2ModelList(w http.ResponseWriter, r *http.Request) {
	reg := provider.BuildRegistry(config.Load(s.workdir))
	var data []modelV2Info

	for _, p := range reg.Providers {
		for mid, mraw := range p.Models {
			m, ok := mraw.(map[string]any)
			if !ok {
				continue
			}
			info := modelV2Info{
				ID:         p.ID + "/" + mid,
				ProviderID: p.ID,
				Name:       mid,
				Enabled:    true,
			}
			if name, ok := m["name"].(string); ok {
				info.Name = name
			}
			info.Capabilities.Tools = true
			info.Capabilities.Input = []string{"text", "image"}
			info.Capabilities.Output = []string{"text", "tool"}

			if limit, ok := m["limit"].(map[string]any); ok {
				if c, ok := limit["context"].(float64); ok {
					info.Limit.Context = int(c)
				}
				if o, ok := limit["output"].(float64); ok {
					info.Limit.Output = int(o)
				}
			}
			// Cost from config if present
			if costRaw, ok := m["cost"].(map[string]any); ok {
				info.Cost = []map[string]any{costRaw}
			} else {
				info.Cost = []map[string]any{{"input": 0.0, "output": 0.0, "cache": map[string]any{"read": 0.0, "write": 0.0}}}
			}
			info.API.ID = mid
			info.API.Type = "aisdk"
			info.API.Package = ""
			// Headers from config
			if headersRaw, ok := m["headers"].(map[string]any); ok {
				h := make(map[string]string)
				for k, v := range headersRaw {
					if s, ok := v.(string); ok {
						h[k] = s
					}
				}
				info.Request.Headers = h
			} else {
				info.Request.Headers = map[string]string{}
			}
			info.Request.Body = map[string]any{}
			// Variants from config
			if variantsRaw, ok := m["variants"].([]any); ok {
				info.Variants = variantsRaw
			} else {
				info.Variants = []any{}
			}
			info.Status = "active"
			data = append(data, info)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"location": map[string]any{"directory": s.workdir},
		"data":     data,
	})
}

type providerV2Info struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Enabled any      `json:"enabled"` // false | {"via":"env","name":"VAR"}
	Env     []string `json:"env"`
	API     struct {
		Type    string `json:"type"`
		Package string `json:"package"`
	} `json:"api"`
	Request struct {
		Headers map[string]string `json:"headers"`
		Body    map[string]any    `json:"body"`
	} `json:"request"`
}

func (s *Server) handleV2ProviderList(w http.ResponseWriter, r *http.Request) {
	reg := provider.BuildRegistry(config.Load(s.workdir))
	var data []providerV2Info
	for _, p := range reg.Providers {
		info := providerV2Info{
			ID:   p.ID,
			Name: p.Name,
			Env:  p.Env,
		}
		info.API.Type = "aisdk"
		info.API.Package = ""
		info.Request.Headers = map[string]string{}
		info.Request.Body = map[string]any{}

		isConnected := false
		for _, cid := range reg.Connected {
			if cid == p.ID {
				isConnected = true
				break
			}
		}
		if isConnected {
			if len(p.Env) > 0 {
				info.Enabled = map[string]any{"via": "env", "name": p.Env[0]}
			} else {
				info.Enabled = true
			}
		} else {
			info.Enabled = false
		}
		data = append(data, info)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"location": map[string]any{"directory": s.workdir},
		"data":     data,
	})
}

func (s *Server) handleV2ProviderGet(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("providerID")
	reg := provider.BuildRegistry(config.Load(s.workdir))

	var target *provider.ProviderInfo
	for i := range reg.Providers {
		if reg.Providers[i].ID == providerID {
			target = &reg.Providers[i]
			break
		}
	}

	if target == nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	info := providerV2Info{
		ID:   target.ID,
		Name: target.Name,
		Env:  target.Env,
	}
	info.API.Type = "aisdk"
	info.API.Package = ""
	info.Request.Headers = map[string]string{}
	info.Request.Body = map[string]any{}

	isConnected := false
	for _, cid := range reg.Connected {
		if cid == target.ID {
			isConnected = true
			break
		}
	}
	if isConnected {
		if len(target.Env) > 0 {
			info.Enabled = map[string]any{"via": "env", "name": target.Env[0]}
		} else {
			info.Enabled = true
		}
	} else {
		info.Enabled = false
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"location": map[string]any{"directory": s.workdir},
		"data":     info,
	})
}

func (s *Server) handleV2SessionEvent(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, ok := s.store.GetSession(sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Subscribe FIRST so the live stream captures all events during state reads and flushes.
	sub, cancel := s.bus.SubscribeFiltered(func(ev event.Event) bool {
		return eventSessionID(ev) == sessionID
	})
	defer cancel()

	// Map to track what we've already sent to avoid obvious duplicates from the IO window.
	sentEvents := make(map[string]bool)

	// Synthesise state-restore events from persisted store before joining live stream.
	if !s.writeEvent(w, flusher, event.NewServerConnected(), event.KindEvent, "") {
		return
	}
	if sess, ok := s.store.GetSession(sessionID); ok {
		ev := event.NewSessionUpdated(sessionID, sess)
		s.writeEvent(w, flusher, ev, event.KindEvent, "")
		sentEvents[ev.ID] = true
	}
	
	// Read store outside the lock: no session-queue state needed here.
	var replayMsgs []session.MessageWithParts
	if msgs, ok := s.store.Messages(sessionID); ok {
		replayMsgs = msgs
	}
	for _, m := range replayMsgs {
		ev := event.NewMessageUpdated(sessionID, m.Info, m.Info.Time.Completed != nil)
		s.writeEvent(w, flusher, ev, event.KindEvent, "")
		sentEvents[ev.ID] = true
		for _, p := range m.Parts {
			evP := event.NewMessagePartUpdated(sessionID, p, 0) // 0 time to let TUI know it's historical
			s.writeEvent(w, flusher, evP, event.KindEvent, "")
			sentEvents[evP.ID] = true
		}
	}

	s.sesMu.Lock()
	work := s.sesQueue[sessionID]
	isBusy := work != nil && work.running
	s.sesMu.Unlock()

	if isBusy {
		evSt := event.NewSessionStatus(sessionID, map[string]string{"type": "busy"})
		s.writeEvent(w, flusher, evSt, event.KindEvent, "")
		sentEvents[evSt.ID] = true
		for i := len(replayMsgs) - 1; i >= 0; i-- {
			if replayMsgs[i].Info.Role == "user" {
				text := partsText(replayMsgs[i].Parts, "text")
				evP := event.NewSessionNextPrompted(sessionID, replayMsgs[i].Info.ID, text, "queue")
				s.writeEvent(w, flusher, evP, event.KindEvent, "")
				sentEvents[evP.ID] = true

				evA := event.NewSessionNextPromptAdmitted(sessionID, replayMsgs[i].Info.ID, text, "queue")
				s.writeEvent(w, flusher, evA, event.KindEvent, "")
				sentEvents[evA.ID] = true
				break
			}
		}
	} else {
		evSt := event.NewSessionStatus(sessionID, map[string]string{"type": "idle"})
		s.writeEvent(w, flusher, evSt, event.KindEvent, "")
		sentEvents[evSt.ID] = true

		evI := event.NewSessionIdle(sessionID)
		s.writeEvent(w, flusher, evI, event.KindEvent, "")
		sentEvents[evI.ID] = true
	}

	for _, req := range s.perms.List() {
		if req.SessionID == sessionID {
			askObj := map[string]any{
				"id":        req.ID,
				"sessionID": sessionID,
				"type":      req.Permission,
				"tool":      req.Permission,
				"pattern":   "",
				"always":    []any{},
			}
			evAsk := event.NewPermissionAsked(askObj)
			s.writeEvent(w, flusher, evAsk, event.KindEvent, "")
			sentEvents[evAsk.ID] = true
		}
	}

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case ev, ok := <-sub.Events():
			if !ok {
				return
			}
			if !eventBelongsToSession(ev, sessionID) {
				continue
			}
			if sentEvents[ev.ID] {
				continue
			}
			if !s.writeEvent(w, flusher, ev, event.KindEvent, "") {
				return
			}
		case <-ticker.C:
			if err := event.WriteHeartbeat(w, flusher); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func eventBelongsToSession(ev event.Event, sessionID string) bool {
	return eventSessionID(ev) == sessionID
}

// handleV2GlobalEvent serves GET /api/event — the v2 global SSE stream.
// It mirrors /global/event but sends bare events (no {directory, payload} wrapper).
func (s *Server) handleV2GlobalEvent(w http.ResponseWriter, r *http.Request) {
	s.serveSSE(w, r, event.KindEvent, directoryOf(r))
}

// handleV2CommandList serves GET /api/command — returns empty array stub.
func (s *Server) handleV2CommandList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}

// handleV2SkillList serves GET /api/skill — returns empty array stub.
func (s *Server) handleV2SkillList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}

// handleV2PermissionRequestList serves GET /api/permission/request.
func (s *Server) handleV2PermissionRequestList(w http.ResponseWriter, r *http.Request) {
	list := s.perms.List()
	data := make([]any, 0, len(list))
	for _, req := range list {
		data = append(data, map[string]any{
			"id":        req.ID,
			"sessionID": req.SessionID,
			"tool":      req.Permission,
			"type":      req.Permission,
			"title":     "Allow tool: " + req.Permission,
			"metadata":  map[string]any{},
			"time":      map[string]any{"created": time.Now().UnixMilli()},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// handleV2PermissionSavedList serves GET /api/permission/saved.
func (s *Server) handleV2PermissionSavedList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}

// handleV2PermissionSavedDelete serves DELETE /api/permission/saved/{id}.
func (s *Server) handleV2PermissionSavedDelete(w http.ResponseWriter, r *http.Request) {
    writeError(w, http.StatusNotImplemented, "not implemented: permission saved delete")
}

func (s *Server) handleV2SessionPermissionRequestList(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, ok := s.store.GetSession(sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	list := s.perms.List()
	data := make([]any, 0)
	for _, req := range list {
		if req.SessionID != sessionID {
			continue
		}
		data = append(data, map[string]any{
			"id":        req.ID,
			"sessionID": req.SessionID,
			"tool":      req.Permission,
			"type":      req.Permission,
			"title":     "Allow tool: " + req.Permission,
			"metadata":  map[string]any{},
			"time":      map[string]any{"created": time.Now().UnixMilli()},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (s *Server) handleV2QuestionRequestList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}

func (s *Server) handleV2SessionQuestionReply(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, ok := s.store.GetSession(sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": true})
}

func (s *Server) handleV2SessionQuestionReject(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, ok := s.store.GetSession(sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": true})
}

// handleV2SessionContext serves GET /api/session/{sessionID}/context. Upstream
// returns the messages currently in the model's context window — i.e. those
// after the last active compaction boundary (all messages when uncompacted).
// (DCP block/stat detail lives at /api/session/{sessionID}/dcp/context.)
func (s *Server) handleV2SessionContext(w http.ResponseWriter, r *http.Request) {
    sessionID := r.PathValue("sessionID")
    msgs, ok := s.store.Messages(sessionID)
    if !ok {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }
    start := 0
    for _, b := range s.store.CompressionBlocks(sessionID) {
        if b.Active && b.EndIndex+1 > start {
            start = b.EndIndex + 1
        }
    }
    if start > len(msgs) {
        start = len(msgs)
    }
    data := make([]any, 0, len(msgs)-start)
    for i := start; i < len(msgs); i++ {
        data = append(data, s.mapToV2Message(msgs[i]))
    }
    writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// handleV2SessionCompact serves POST /api/session/{sessionID}/compact.
func (s *Server) handleV2SessionCompact(w http.ResponseWriter, r *http.Request) {
    sessionID := r.PathValue("sessionID")
    if _, ok := s.store.GetSession(sessionID); !ok {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }
    var body compactRequest
    _ = decodeBody(r, &body)
    block, stats, err := s.compactSession(sessionID, body)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    // compactSession already publishes the compaction lifecycle + legacy
    // session.compact/compacted events; do not re-publish here (was a double-emit).
    writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"block": block, "stats": stats}})
}

// handleV2SessionPermissionReply serves POST /api/session/{sessionID}/permission/request/{requestID}/reply.
func (s *Server) handleV2SessionPermissionReply(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	requestID := r.PathValue("requestID")
	if _, ok := s.store.GetSession(sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	var body struct {
		Response string `json:"response"`
		Decision string `json:"decision"`
	}
	_ = decodeBody(r, &body)
	reply := body.Response
	if reply == "" {
		reply = body.Decision
	}
	if reply == "" {
		reply = "once"
	}
	if err := s.perms.Reply(requestID, reply); err != nil {
		writeError(w, http.StatusNotFound, "permission request not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": true})
}

func (s *Server) resolveWorkdirPath(rawPath string) (string, error) {
	if rawPath == "" {
		return s.workdir, nil
	}
	abs := rawPath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.workdir, abs)
	}
	clean := filepath.Clean(abs)
	if clean != s.workdir && !strings.HasPrefix(clean, s.workdir+string(filepath.Separator)) {
		return "", fmt.Errorf("path outside workdir")
	}
	return clean, nil
}

// handleV2FSList serves GET /api/fs/list — lists directory contents.
func (s *Server) handleV2FSList(w http.ResponseWriter, r *http.Request) {
	path, err := s.resolveWorkdirPath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		data = append(data, map[string]any{
			"name": e.Name(),
			"type": map[bool]string{true: "directory", false: "file"}[e.IsDir()],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// handleV2FSRead serves GET /api/fs/read — reads a file.
func (s *Server) handleV2FSRead(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("path")
	if raw == "" {
		writeJSON(w, http.StatusOK, map[string]any{"data": ""})
		return
	}
	path, err := s.resolveWorkdirPath(raw)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	content, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": string(content)})
}
