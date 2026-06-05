package session

// Time holds creation/update/completion timestamps in epoch milliseconds.
// completed is a pointer so it can serialize as null until the turn finishes.
type Time struct {
	Created   int64  `json:"created"`
	Updated   int64  `json:"updated,omitempty"`
	Completed *int64 `json:"completed"`
}

// Session mirrors the schema fields the bot + TUI read (architecture §2.2).
type Session struct {
	ID        string      `json:"id"` // ses_*
	ParentID  string      `json:"parentID,omitempty"`
	Title     string      `json:"title"`
	Directory string      `json:"directory"`
	Time      SessionTime `json:"time"`
}

// SessionTime holds session created/updated timestamps (ms).
type SessionTime struct {
	Created int64 `json:"created"`
	Updated int64 `json:"updated"`
}

// Message is the info block of a message (architecture Appendix A).
type Message struct {
	ID        string `json:"id"` // msg_*
	Role      string `json:"role"`
	SessionID string `json:"sessionID"`
	Time      Time   `json:"time"`
}

// Part is a single content part of a message.
type Part struct {
	ID        string `json:"id"` // prt_*
	MessageID string `json:"messageID"`
	SessionID string `json:"sessionID"`
	Type      string `json:"type"` // "text" | "reasoning" | ...
	Text      string `json:"text,omitempty"`
}

// MessageWithParts is the {info, parts} shape returned by GET .../message.
type MessageWithParts struct {
	Info  Message `json:"info"`
	Parts []Part  `json:"parts"`
}
