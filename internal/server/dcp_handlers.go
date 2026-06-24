
package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencode-go/opencode-go/internal/config"
	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/session"
	"github.com/opencode-go/opencode-go/internal/provider"
	"net/http"
	"strings"
	"time"
)

type compactRequest struct {
	Mode       string `json:"mode"`
	Focus      string `json:"focus"`
	KeepRecent int    `json:"keepRecent"`
	// Reason is "auto" (overflow-triggered) or "manual" (explicit). Defaults to
	// "manual"; it flows into the session.next.compaction.* events.
	Reason string `json:"reason"`
}

func (s *Server) compactSession(sessionID string, body compactRequest) (session.CompressionBlock, map[string]any, error) {
	reason := body.Reason
	if reason != "auto" {
		reason = "manual"
	}
	// Synthetic message ID linking the started/delta/ended sequence (upstream parity).
	compactionMsgID := session.NewID("msg")
	var endedText, endedRecent string

	// Mark the session as compacting so the TUI shows its working indicator
	// (TUI keys off session.time.compacting). Cleared on completion below.
	now := time.Now().UnixMilli()
	if upd, ok := s.store.SetSessionCompacting(sessionID, &now); ok {
		s.bus.Publish(event.NewSessionUpdated(sessionID, upd))
	}
	// Canonical upstream compaction lifecycle the TUI listens for.
	s.bus.Publish(event.NewSessionNextCompactionStarted(sessionID, compactionMsgID, reason))
	// Legacy Go-port events retained for older clients.
	s.bus.Publish(event.NewCompactionStarted(sessionID))
	defer func() {
		if upd, ok := s.store.SetSessionCompacting(sessionID, nil); ok {
			s.bus.Publish(event.NewSessionUpdated(sessionID, upd))
		}
		s.bus.Publish(event.NewSessionNextCompactionEnded(sessionID, compactionMsgID, reason, endedText, endedRecent))
		s.bus.Publish(event.NewCompactionEnded(sessionID))
	}()

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
	// Build raw lines and original char count
	var origChars int
	var rawBuilder strings.Builder
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
		line := role + ": " + text
		origChars += len(line)
		rawBuilder.WriteString(line)
		rawBuilder.WriteByte('\n')
	}
	if len(compressMsgs) == 0 {
		return session.CompressionBlock{}, s.store.DCPStats(sessionID), nil
	}
	rawText := strings.TrimSuffix(rawBuilder.String(), "\n")
	// Model summarization
	prompt := "Summarize the following conversation concisely, preserving key facts, decisions, and context:\n\n" + rawText
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req := provider.ChatRequest{
		Model: s.model,
		Messages: []provider.ChatMessage{{Role: "user", Content: provider.TextContent(prompt)}},
	}
	stream, err := s.provider.StreamChat(ctx, req)
	if err != nil {
		return session.CompressionBlock{}, nil, fmt.Errorf("compact summary model error: %w", err)
	}
	var summaryBuilder strings.Builder
	for chunk := range stream {
		if chunk.Err != nil {
			return session.CompressionBlock{}, nil, fmt.Errorf("compact summary stream error: %w", chunk.Err)
		}
		summaryBuilder.WriteString(chunk.TextDelta)
		if chunk.TextDelta != "" {
			s.bus.Publish(event.NewSessionNextCompactionDelta(sessionID, compactionMsgID, chunk.TextDelta))
		}
	}
	modelSummary := strings.TrimSpace(summaryBuilder.String())
	summary := "DCP compression summary\n" + modelSummary

	// Populate the terminal compaction.ended payload: full summary + the recent
	// messages kept verbatim past the compaction boundary.
	endedText = modelSummary
	if body.KeepRecent > 0 && len(msgs) > body.KeepRecent {
		var recentBuilder strings.Builder
		for _, m := range msgs[len(msgs)-body.KeepRecent:] {
			for _, p := range m.Parts {
				if p.Type == "text" && p.Text != "" {
					recentBuilder.WriteString(m.Info.Role + ": " + p.Text + "\n")
					break
				}
			}
		}
		endedRecent = strings.TrimSuffix(recentBuilder.String(), "\n")
	}
	block := session.CompressionBlock{ID: session.NewID("dcp"), SessionID: sessionID, Mode: body.Mode, Summary: summary, StartIndex: 0, EndIndex: endIndex, OriginalCount: len(compressMsgs), OriginalChars: origChars, MessageID: compressMsgs[len(compressMsgs)-1].Info.ID, Created: time.Now().UnixMilli(), Focus: body.Focus, Active: true}
	s.store.AddCompressionBlock(sessionID, block)
	if len(compressMsgs) > 0 {
		// Emit compact & compacted events
		s.bus.Publish(event.NewSessionCompact(sessionID, block, s.store.DCPStats(sessionID)))
		s.bus.Publish(event.NewSessionCompacted(sessionID))
	}

	return block, s.store.DCPStats(sessionID), nil
}

// handleSessionCompact serves POST /session/{id}/compact (v1). Mirrors the v2 endpoint.
func (s *Server) handleSessionCompact(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	var body compactRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	block, stats, err := s.compactSession(sessionID, body)
	if err != nil {
		if strings.Contains(err.Error(), "session not found") {
			writeError(w, http.StatusNotFound, "session not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
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
	goals, _ := s.store.GetGoals(sessionID)
	todos, _ := s.store.GetTodos(sessionID)
	msgs, ok := s.store.Messages(sessionID)
	msgCount := 0
	if ok {
		msgCount = len(msgs)
	}
	sess, _ := s.store.GetSession(sessionID)
	data := map[string]any{"sessionID": sessionID, "blocks": s.store.CompressionBlocks(sessionID), "stats": s.store.DCPStats(sessionID), "goals": goals, "todos": todos, "messageCount": msgCount, "session": sess}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
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
