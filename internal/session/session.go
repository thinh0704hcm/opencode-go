package session

// Time holds creation/update/completion timestamps in epoch milliseconds.
// completed is a pointer so it can serialize as null until the turn finishes.
type Time struct {
	Created   int64  `json:"created"`
	Updated   int64  `json:"updated,omitempty"`
	Completed *int64 `json:"completed,omitempty"`
}

// Session mirrors the schema fields the bot + TUI read (architecture §2.2).
type Session struct {
	ID        string      `json:"id"` // ses_*
	Slug      string      `json:"slug"`
	Version   string      `json:"version"`
	ProjectID string      `json:"projectID"`
	ParentID  string      `json:"parentID,omitempty"`
	Title     string      `json:"title"`
	Directory string      `json:"directory"`
    Revert *RevertInfo `json:"revert,omitempty"`
	Time      SessionTime `json:"time"`
}

// RevertInfo stores metadata about a pending revert stash.
type RevertInfo struct {
    MessageID string `json:"messageID"`
    PartID    string `json:"partID,omitempty"`
}

// SessionTime holds session created/updated timestamps (ms). Compacting is set
// to a timestamp while a compaction is in progress and cleared when it finishes;
// the TUI shows its "compacting" working indicator while this is non-nil.
type SessionTime struct {
	Created    int64  `json:"created"`
	Updated    int64  `json:"updated"`
	Compacting *int64 `json:"compacting,omitempty"`
}

// Tokens holds assistant token accounting (SDK AssistantMessage shape).
type Tokens struct {
	Total     int64      `json:"total"`
	Input     int64      `json:"input"`
	Output    int64      `json:"output"`
	Reasoning int64      `json:"reasoning"`
	Cache     TokenCache `json:"cache"`
}

// TokenCache holds cache read/write token counts.
type TokenCache struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

// MsgPath holds the assistant message's cwd/root (SDK AssistantMessage shape).
type MsgPath struct {
	Cwd  string `json:"cwd"`
	Root string `json:"root"`
}

// Message is the info block of a message (architecture Appendix A).
// ModelID/ProviderID/Mode/Cost/Tokens/Path are assistant-only optional fields
// (omitempty) so user messages stay clean while the TUI can read tokens.output.
type Message struct {
	GlobalSeq  uint64      `json:"globalSeq,omitempty"`
	ID         string      `json:"id"` // msg_*
	Role       string      `json:"role"`
	SessionID  string      `json:"sessionID"`
	Time       Time        `json:"time"`
	Agent      string      `json:"agent"`
	ParentID   string      `json:"parentID"`
	ModelID    string      `json:"modelID,omitempty"`
	ProviderID string      `json:"providerID,omitempty"`
	Mode       string      `json:"mode,omitempty"`
	Finish     string      `json:"-"`
	Cost       *float64    `json:"cost,omitempty"`
	Tokens     *Tokens     `json:"tokens,omitempty"`
	Path       *MsgPath    `json:"path,omitempty"`
	Model      *MsgModel   `json:"model,omitempty"`   // user message model
	Summary    *MsgSummary `json:"summary,omitempty"` // user message summary
	Hidden     bool        `json:"hidden,omitempty"`  // sub-agent inline messages excluded from history
}

// MsgModel is the user message's {providerID, modelID} block.
type MsgModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// MsgSummary is the user message's summary block (TUI turn grouping).
type MsgSummary struct {
	Diffs []any `json:"diffs"`
}

// Part is a single content part of a message.
type Part struct {
	GlobalSeq       uint64     `json:"globalSeq,omitempty"`
	ID              string     `json:"id"` // prt_*
	MessageID       string     `json:"messageID"`
	SessionID       string     `json:"sessionID"`
	Type            string     `json:"type"` // "text" | "reasoning" | "tool" | "step-start" | "step-finish" | "subtask"
	Text            string     `json:"text,omitempty"`
	Tool            string     `json:"tool,omitempty"`
	CallID          string     `json:"callID,omitempty"`
	Prompt          string     `json:"prompt,omitempty"`
	Description     string     `json:"description,omitempty"`
	Agent           string     `json:"agent,omitempty"`
	TargetSessionID string     `json:"targetSessionID,omitempty"`
	Model           *PartModel `json:"model,omitempty"`
	Command         string     `json:"command,omitempty"`
	State           *PartState `json:"state,omitempty"`
	// Time is set on assistant text parts (start, optional end); real user text
	// parts carry no time, so this stays omitempty to preserve that asymmetry.
	Time *PartTime `json:"time,omitempty"`
	// Reason/Cost/Tokens are set on step-finish parts.
	Reason string   `json:"reason,omitempty"`
	Cost   *float64 `json:"cost,omitempty"`
	Tokens *Tokens  `json:"tokens,omitempty"`
}

// PartModel holds provider and model IDs for subtask parts.
type PartModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// PartTime holds a part's start (and optional end) timestamps in epoch ms.
// Assistant text parts carry it; user text parts do not.
type PartTime struct {
	Start int64  `json:"start"`
	End   *int64 `json:"end,omitempty"`
}

// PartState holds tool-part execution status for "tool" parts. The shape
// mirrors real opencode 1.16.0 so the TUI's tool renderer can read
// state.input/metadata/time without hitting undefined fields. Title always
// serializes as "" like real; Input/Metadata/Time stay omitempty.
type PartState struct {
	Status   string         `json:"status"` // pending|running|completed|error
	Input    map[string]any `json:"input,omitempty"`
	Output   string         `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
	Title    string         `json:"title"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Time     *PartStateTime `json:"time,omitempty"`
}

// PartStateTime holds a tool-part's start (and optional end) timestamps in ms.
type PartStateTime struct {
	Start int64  `json:"start"`
	End   *int64 `json:"end,omitempty"`
}

// MessageWithParts is the {info, parts} shape returned by GET .../message.
type MessageWithParts struct {
	Info  Message `json:"info"`
	Parts []Part  `json:"parts"`
}
