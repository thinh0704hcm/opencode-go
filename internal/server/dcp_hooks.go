package server

import (
	"regexp"

	"github.com/opencode-go/opencode-go/internal/config"
	"github.com/opencode-go/opencode-go/internal/provider"
)

var (
	dcpPairedTag   = regexp.MustCompile(`(?is)<dcp[^>]*>.*?</dcp[^>]*>`)
	dcpUnpairedTag = regexp.MustCompile(`(?is)</?dcp[^>]*>`)
)

type DCPHooks struct{ srv *Server }

func (s *Server) DCPHooks() DCPHooks { return DCPHooks{srv: s} }

func (h DCPHooks) SystemPrompt(workdir, current string) string {
	cfg := config.Load(workdir).DCP()
	if !cfg.Enabled || cfg.CompressPermission == "deny" {
		return current
	}
	return current + "\n\n" + loadDCPPrompts(workdir, cfg).System
}

func (h DCPHooks) ChatMessages(workdir, sessionID string, messages []provider.ChatMessage) []provider.ChatMessage {
	cfg := config.Load(workdir).DCP()
	if !cfg.Enabled && len(h.srv.store.CompressionBlocks(sessionID)) == 0 {
		return messages
	}
	return h.srv.applyDCPPruning(messages, sessionID)
}

func (h DCPHooks) TextComplete(text string) string { return stripDCPHallucinations(text) }

func stripDCPHallucinations(text string) string {
	text = dcpPairedTag.ReplaceAllString(text, "")
	return dcpUnpairedTag.ReplaceAllString(text, "")
}
