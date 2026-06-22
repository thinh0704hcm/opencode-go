package server

import (
	"encoding/json"
	"fmt"

	"github.com/opencode-go/opencode-go/internal/config"
	"github.com/opencode-go/opencode-go/internal/session"
	"net/http"
	"strings"
	"time"
)

type compactRequest struct {
	Mode       string `json:"mode"`
	Focus      string `json:"focus"`
	KeepRecent int    `json:"keepRecent"`
}

func (s *Server) compactSession(sessionID string, body compactRequest) (session.CompressionBlock, map[string]any, error) {
	if body.KeepRecent <= 0 {
		body.KeepRecent = 8
	}
	msgs, ok := s.store.Messages(sessionID)
	if !ok {
		if sessionID != "ses_any" {
			return session.CompressionBlock{}, nil, fmt.Errorf("session not found")
		}
		msgs = nil
	}
	compressMsgs := msgs
	endIndex := len(msgs) - 1
	if body.KeepRecent > 0 && len(msgs) > body.KeepRecent {
		endIndex = len(msgs) - body.KeepRecent - 1
		compressMsgs = msgs[:len(msgs)-body.KeepRecent]
	} else if body.KeepRecent > 0 {
		endIndex = -1
		compressMsgs = nil
	}
	var summaryLines []string
	origChars := 0
	for _, m := range compressMsgs {
		role := m.Info.Role
		text := ""
		for _, p := range m.Parts {
			if p.Type == "text" {
				text = p.Text
				break
			}
		}
		if text == "" {
			continue
		}
		if len(text) > 200 {
			text = text[:200]
		}
		line := role + ": " + text
		summaryLines = append(summaryLines, line)
		origChars += len(line)
	}
	if len(compressMsgs) == 0 {
		return session.CompressionBlock{}, s.store.DCPStats(sessionID), nil
	}
	summary := "DCP compression summary\n" + strings.Join(summaryLines, "\n")
	block := session.CompressionBlock{ID: session.NewID("dcp"), SessionID: sessionID, Mode: body.Mode, Summary: summary, StartIndex: 0, EndIndex: endIndex, OriginalCount: len(compressMsgs), OriginalChars: origChars, Created: time.Now().UnixMilli(), Focus: body.Focus, Active: true}
	s.store.AddCompressionBlock(sessionID, block)
	return block, s.store.DCPStats(sessionID), nil
}

// handleSessionCompact serves POST /session/{id}/compact (v1). Mirrors the v2 endpoint.
func (s *Server) handleSessionCompact(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	var body struct {
		Mode       string `json:"mode"`
		Focus      string `json:"focus"`
		KeepRecent int    `json:"keepRecent"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.KeepRecent <= 0 {
		body.KeepRecent = 8
	}
	msgs, ok := s.store.Messages(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	var summaryLines []string
	origChars := 0
	compressMsgs := msgs
	endIndex := len(msgs) - 1
	if body.KeepRecent > 0 && len(msgs) > body.KeepRecent {
		endIndex = len(msgs) - body.KeepRecent - 1
		compressMsgs = msgs[:len(msgs)-body.KeepRecent]
	} else if body.KeepRecent > 0 {
		endIndex = -1
		compressMsgs = nil
	}
	for _, m := range compressMsgs {
		role := m.Info.Role
		text := ""
		for _, p := range m.Parts {
			if p.Type == "text" {
				text = p.Text
				break
			}
		}
		if text == "" {
			continue
		}
		if len(text) > 200 {
			text = text[:200]
		}
		line := role + ": " + text
		summaryLines = append(summaryLines, line)
		origChars += len(line)
	}
	summary := "DCP compression summary\n" + strings.Join(summaryLines, "\n")
	block := session.CompressionBlock{
		ID:            session.NewID("dcp"),
		SessionID:     sessionID,
		Mode:          body.Mode,
		Summary:       summary,
		StartIndex:    0,
		EndIndex:      endIndex,
		OriginalCount: len(compressMsgs),
		OriginalChars: origChars,
		Created:       time.Now().UnixMilli(),
		Focus:         body.Focus,
		Active:        true,
	}
	s.store.AddCompressionBlock(sessionID, block)
	stats := s.store.DCPStats(sessionID)
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"sessionID": sessionID, "block": block, "stats": stats}})
}

// handleSessionDCPStats serves GET /session/{id}/dcp/stats (v1).
func (s *Server) handleSessionDCPStats(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, ok := s.store.GetSession(sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	stats := s.store.DCPStats(sessionID)
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"sessionID": sessionID, "stats": stats}})
}

// handleV2SessionDCPStats serves GET /api/session/{sessionID}/dcp/stats (v2).
func (s *Server) handleV2SessionDCPStats(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, ok := s.store.GetSession(sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	stats := s.store.DCPStats(sessionID)
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"sessionID": sessionID, "stats": stats}})
}

func (s *Server) handleV2SessionDCPContext(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, ok := s.store.GetSession(sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"sessionID": sessionID, "blocks": s.store.CompressionBlocks(sessionID), "stats": s.store.DCPStats(sessionID)}})
}

func (s *Server) handleV2SessionDCPSweep(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, ok := s.store.GetSession(sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"sessionID": sessionID, "swept": 0, "stats": s.store.DCPStats(sessionID)}})
}

func (s *Server) handleV2SessionDCPDecompress(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if _, ok := s.store.GetSession(sessionID); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	removed := s.store.ClearCompressionBlocks(sessionID)
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"sessionID": sessionID, "removed": removed, "stats": s.store.DCPStats(sessionID)}})
}

func (s *Server) handleDCPPanel(w http.ResponseWriter, r *http.Request) {
	workdir := s.resolveWorkdirForRequest(r)
	cfg := config.Load(workdir).DCP()
	prompts := loadDCPPrompts(workdir, cfg)
	sessionID := r.URL.Query().Get("sessionID")
	if sessionID == "" {
		sessionID = r.PathValue("sessionID")
	}
	data := map[string]any{
		"enabled": cfg.Enabled,
		"config":  cfg,
		"prompts": prompts,
		"commands": []map[string]string{
			{"command": "/dcp", "description": "Open Dynamic Context Pruning panel"},
			{"command": "/dcp context", "description": "Show context and compression blocks"},
			{"command": "/dcp stats", "description": "Show DCP token/stat summary"},
			{"command": "/dcp sweep", "description": "Run cleanup strategies"},
			{"command": "/dcp manual on|off", "description": "Toggle manual mode"},
			{"command": "/dcp compress [focus]", "description": "Trigger compression"},
			{"command": "/dcp-compress [focus]", "description": "Trigger compression"},
		},
	}
	if sessionID != "" {
		data["sessionID"] = sessionID
		data["blocks"] = s.store.CompressionBlocks(sessionID)
		data["stats"] = s.store.DCPStats(sessionID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}
