package server

import (
    "github.com/opencode-go/opencode-go/internal/config"
    "github.com/opencode-go/opencode-go/internal/session"
)

// isDCPOverflow checks if the session has exceeded the DCP token budget.
// Returns true if compaction should be triggered.
func (s *Server) isDCPOverflow(workdir string, tokens *session.Tokens) bool {
    cfg := config.Load(workdir).DCP()
    if !cfg.Auto || !cfg.Enabled {
        return false
    }
    if cfg.ContextLimit == 0 {
        return false
    }
    // count tokens
    var count int64
    if tokens != nil {
        count = tokens.Input + tokens.Output + tokens.Reasoning + tokens.Cache.Read + tokens.Cache.Write
    }
    if count == 0 {
        return false
    }
    // reserved tokens for output limit or 20000
    reserved := int64(20000)
    if cfg.OutputLimit > 0 && int64(cfg.OutputLimit) < reserved {
        reserved = int64(cfg.OutputLimit)
    }
    usable := int64(cfg.ContextLimit) - reserved
    if usable <= 0 {
        return false
    }
    return count >= usable
}
