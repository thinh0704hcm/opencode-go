package server

import (
	"encoding/base64"
	"net/http"
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
	ID       string  `json:"id"`
	ParentID string  `json:"parentID,omitempty"`
	Cost     float64 `json:"cost"`
	Tokens   struct {
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

func (s *Server) handleV2SessionList(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil {
			limit = l
		}
	}
	sessions := s.store.List()
	total := len(sessions)
	offset := 0
	if cur := r.URL.Query().Get("cursor"); cur != "" {
		if b, err := base64.StdEncoding.DecodeString(cur); err == nil {
			offset, _ = strconv.Atoi(string(b))
		}
	}
	if offset > total {
		offset = total
	}
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
	info := sessionV2Info{
		ID:       sess.ID,
		ParentID: sess.ParentID,
		Title:    sess.Title,
	}
	info.Time.Created = sess.Time.Created
	info.Time.Updated = sess.Time.Updated
	info.Location.Directory = sess.Directory

	if msgs, ok := s.store.Messages(sess.ID); ok {
		for _, m := range msgs {
			info.Cost += m.Info.Cost
			if m.Info.Tokens != nil {
				info.Tokens.Input += m.Info.Tokens.Input
				info.Tokens.Output += m.Info.Tokens.Output
				info.Tokens.Reasoning += m.Info.Tokens.Reasoning
				info.Tokens.Cache.Read += m.Info.Tokens.Cache.Read
				info.Tokens.Cache.Write += m.Info.Tokens.Cache.Write
			}
		}
	}
	return info
}

type v2CreateSessionRequest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
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
	Delivery string `json:"delivery"` // "steer" | "queue"; default "steer"
	Resume   bool   `json:"resume"`
}

type sessionInputAdmitted struct {
	AdmittedSeq int64  `json:"admittedSeq"`
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
	agent, _ := resolveAgent(s.workdir, "") // Use default agent
	if agent.Name == "" {
		agent.Name = "build"
	}

	// Append user message
	msg, ok := s.store.AppendUserMessage(sessionID, msgID, s.provider.ID(), s.model, agent.Name, texts)
	if !ok {
		writeJSON(w, http.StatusNotFound, sessionNotFoundError)
		return
	}
	if updated, ok := s.store.GetSession(sessionID); ok {
		s.bus.Publish(event.NewSessionUpdated(sessionID, updated))
	}
	s.publishUserMessage(sessionID, msg)

	seq, ok := s.startOrQueue(sessionID, msgID, s.provider.ID(), s.model, texts, images, "", agent, delivery)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]any{
			"_tag":     "ConflictError",
			"message":  "session is busy",
			"resource": "session",
		})
		return
	}

	s.bus.Publish(event.NewSessionNextPromptAdmitted(sessionID, msgID, req.Prompt.Text, delivery, seq))

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

	sub, cancel := s.bus.Subscribe()
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
		case ev := <-sub.Events():
			if ev.Type == event.TypeSessionIdle {
				if eventSessionID(ev) == sessionID {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
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
		if s, ok := p.Info.(session.Session); ok {
			return s.ID
		}
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

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil {
			limit = l
		}
	}
	order := r.URL.Query().Get("order") // "asc" | "desc", default "asc"

	total := len(msgs)
	offset := 0
	if cur := r.URL.Query().Get("cursor"); cur != "" {
		if b, err := base64.StdEncoding.DecodeString(cur); err == nil {
			offset, _ = strconv.Atoi(string(b))
		}
	}
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}

	var data []any
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
			"id":    m.Info.ID,
			"type":  "user",
			"time":  map[string]any{"created": m.Info.Time.Created},
			"text":  partsText(m.Parts, "text"),
			"agent": m.Info.Agent,
			"model": map[string]any{"providerID": providerID, "modelID": modelID},
		}
	}

	var content []any
	for _, p := range m.Parts {
		switch p.Type {
		case "text":
			content = append(content, map[string]any{
				"type": "text",
				"id":   p.ID,
				"text": p.Text,
			})
		case "tool":
			if p.State == nil {
				continue
			}
			state := map[string]any{
				"status": p.State.Status,
				"input":  p.State.Input,
				"output": p.State.Output,
			}
			content = append(content, map[string]any{
				"type":  "tool",
				"id":    p.ID,
				"name":  p.Tool,
				"state": state,
			})
		}
	}

	asst := map[string]any{
		"id":      m.Info.ID,
		"type":    "assistant",
		"time":    map[string]any{"created": m.Info.Time.Created, "completed": m.Info.Time.Completed},
		"agent":   m.Info.Agent,
		"model":   map[string]any{"providerID": m.Info.ProviderID, "modelID": m.Info.ModelID},
		"content": content,
		"finish":  m.Info.Finish,
		"cost":    m.Info.Cost,
	}
	if m.Info.Tokens != nil {
		asst["tokens"] = m.Info.Tokens
	}
	return asst
}

type agentV2Info struct {
	ID          string `json:"id"`
	Mode        string `json:"mode"`
	Hidden      bool   `json:"hidden"`
	Description string `json:"description,omitempty"`
	System      string `json:"system,omitempty"`
	Permissions []any  `json:"permissions"`
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
		data = append(data, agentV2Info{
			ID:          a.Name,
			Mode:        a.Mode,
			Description: a.Description,
			System:      a.Prompt,
			Permissions: []any{},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"location": map[string]any{"directory": s.workdir},
		"data":     data,
	})
}

type modelV2Info struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Capabilities struct {
		Tools  bool     `json:"tools"`
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"capabilities"`
	Limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
	Cost struct {
		Input  float64 `json:"input"`
		Output float64 `json:"output"`
		Cache  struct {
			Read  float64 `json:"read"`
			Write float64 `json:"write"`
		} `json:"cache"`
	} `json:"cost"`
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
				ID:         mid,
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

	sub, cancel := s.bus.SubscribeFiltered(func(ev event.Event) bool {
		return eventSessionID(ev) == sessionID
	})
	defer cancel()

	if !s.writeEvent(w, flusher, event.NewServerConnected(), event.KindEvent, "") {
		return
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
