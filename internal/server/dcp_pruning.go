package server

import (
    "github.com/opencode-go/opencode-go/internal/config"
    "github.com/opencode-go/opencode-go/internal/provider"
)

// applyDCPPruning applies DCP strategies to chat messages.
// It deduplicates tool results and purges errored tool inputs.
func (s *Server) applyDCPPruning(messages []provider.ChatMessage, sessionID string) []provider.ChatMessage {
    cfg := config.Load(s.workdir).DCP()
    if !cfg.Enabled {
        return messages
    }
    return applyDCPStrategies(messages, cfg)
}
