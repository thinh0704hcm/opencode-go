package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/opencode-go/opencode-go/internal/session"
	"github.com/opencode-go/opencode-go/internal/tool"
)

type compressTool struct{ srv *Server }

func (compressTool) Name() string   { return "compress" }
func (compressTool) Mutating() bool { return true }

type compressToolInput struct {
	Topic      string `json:"topic"`
	Mode       string `json:"mode"`
	Focus      string `json:"focus"`
	KeepRecent int    `json:"keepRecent"`
	Content    []struct {
		StartID   string `json:"startId"`
		EndID     string `json:"endId"`
		MessageID string `json:"messageId"`
		Topic     string `json:"topic"`
		Summary   string `json:"summary"`
	} `json:"content"`
}

func (t compressTool) Execute(ctx context.Context, input json.RawMessage, sb *tool.Sandbox) (tool.Result, error) {
	var in compressToolInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tool.Result{}, fmt.Errorf("compress: invalid JSON: %w", err)
	}
	sessionID := sessionIDFromCtx(ctx)
	if sessionID == "" {
		return tool.Result{}, fmt.Errorf("compress: missing session ID in context")
	}
	mode := in.Mode
	if mode == "" {
		mode = "range"
		if len(in.Content) > 0 && in.Content[0].MessageID != "" {
			mode = "message"
		}
	}
	focus := in.Focus
	if focus == "" {
		focus = in.Topic
	}
	var block session.CompressionBlock
	var stats map[string]any
	var err error
	if len(in.Content) > 0 {
		var summaries []string
		startID, endID := "", ""
		for i, item := range in.Content {
			if strings.TrimSpace(item.Summary) == "" {
				continue
			}
			label := item.Topic
			if label == "" {
				label = in.Topic
			}
			if label != "" {
				summaries = append(summaries, label+": "+item.Summary)
			} else {
				summaries = append(summaries, item.Summary)
			}
			if i == 0 {
				startID = firstNonEmpty(item.StartID, item.MessageID)
			}
			endID = firstNonEmpty(item.EndID, item.MessageID, endID)
		}
		if len(summaries) > 0 {
			summary := "[Compressed conversation section]\n" + strings.Join(summaries, "\n\n")
			block = session.CompressionBlock{ID: session.NewID("dcp"), SessionID: sessionID, Mode: mode, Summary: summary, StartID: startID, EndID: endID, OriginalCount: len(summaries), OriginalChars: len(summary), Created: time.Now().UnixMilli(), Focus: focus, Topic: in.Topic, Active: true}
			t.srv.store.AddCompressionBlock(sessionID, block)
			stats = t.srv.store.DCPStats(sessionID)
		} else {
			block, stats, err = t.srv.compactSession(sessionID, compactRequest{Mode: mode, Focus: focus, KeepRecent: in.KeepRecent})
		}
	} else {
		block, stats, err = t.srv.compactSession(sessionID, compactRequest{Mode: mode, Focus: focus, KeepRecent: in.KeepRecent})
	}
	if err != nil {
		return tool.Result{}, err
	}
	resp := map[string]any{"sessionID": sessionID, "block": block, "stats": stats}
	b, _ := json.MarshalIndent(resp, "", "  ")
	return tool.Result{Output: string(b)}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
