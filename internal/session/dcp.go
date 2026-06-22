package session

type CompressionBlock struct {
    ID            string `json:"id"`
    SessionID     string `json:"sessionID"`
    Mode          string `json:"mode"` // "range" | "message" | "auto"
    Summary       string `json:"summary"`
    StartIndex    int    `json:"startIndex"`
    EndIndex      int    `json:"endIndex"`
    StartID       string `json:"startId,omitempty"`
    EndID         string `json:"endId,omitempty"`
    MessageID     string `json:"messageId,omitempty"`
    OriginalCount int    `json:"originalCount"`
    OriginalChars int    `json:"originalChars"`
    Created       int64  `json:"created"`
    Focus         string `json:"focus,omitempty"`
    Topic         string `json:"topic,omitempty"`
    Active        bool   `json:"active"`
}

// AddCompressionBlock appends a compression block to a session.
func (s *Store) AddCompressionBlock(sessionID string, block CompressionBlock) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.dcpBlocks[sessionID] = append(s.dcpBlocks[sessionID], block)
}

// CompressionBlocks returns a copy of all compression blocks for a session.
func (s *Store) CompressionBlocks(sessionID string) []CompressionBlock {
    s.mu.RLock()
    defer s.mu.RUnlock()
    blocks := s.dcpBlocks[sessionID]
    out := make([]CompressionBlock, len(blocks))
    copy(out, blocks)
    return out
}

// ClearCompressionBlocks removes all compression blocks for a session and returns the count.
func (s *Store) ClearCompressionBlocks(sessionID string) int {
    s.mu.Lock()
    defer s.mu.Unlock()
    n := len(s.dcpBlocks[sessionID])
    delete(s.dcpBlocks, sessionID)
    return n
}

// DCPStats returns summary statistics for a session's compression blocks.
func (s *Store) DCPStats(sessionID string) map[string]any {
    s.mu.RLock()
    defer s.mu.RUnlock()
    blocks := s.dcpBlocks[sessionID]
    totalOriginalCount := 0
    totalOriginalChars := 0
    totalSummaryChars := 0
    // token aggregates
    inputTokens := 0
    outputTokens := 0
    reasoningTokens := 0
    cacheReadTokens := 0
    cacheWriteTokens := 0

    for _, b := range blocks {
        totalOriginalCount += b.OriginalCount
        totalOriginalChars += b.OriginalChars
        totalSummaryChars += len(b.Summary)
    }
    // aggregate token counts from messages
    for _, m := range s.messages[sessionID] {
        if m.Info.Tokens != nil {
            t := m.Info.Tokens
            inputTokens += int(t.Input)
            outputTokens += int(t.Output)
            reasoningTokens += int(t.Reasoning)
            cacheReadTokens += int(t.Cache.Read)
            cacheWriteTokens += int(t.Cache.Write)
        }
    }
    totalTokens := inputTokens + outputTokens + reasoningTokens + cacheReadTokens + cacheWriteTokens

    return map[string]any{
        "blocks":         len(blocks),
        "originalCount":  totalOriginalCount,
        "originalChars":  totalOriginalChars,
        "summaryChars":   totalSummaryChars,
        "savedChars":     totalOriginalChars - totalSummaryChars,
        "inputTokens":    inputTokens,
        "outputTokens":   outputTokens,
        "reasoningTokens": reasoningTokens,
        "cacheReadTokens": cacheReadTokens,
        "cacheWriteTokens": cacheWriteTokens,
        "totalTokens":    totalTokens,
    }
}
