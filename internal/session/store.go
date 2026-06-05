package session

import (
	"sync"
	"sync/atomic"
	"time"
)

// Store is an in-memory session/message store guarded by an RWMutex.
// On-disk persistence is NOT implemented for M1 (architecture §2.2).
type Store struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	messages map[string][]*MessageWithParts // ses_* -> ordered messages
}

var idSeq atomic.Uint64

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// NewID produces prefix + "_" + base62(monotonic).
func NewID(prefix string) string {
	n := idSeq.Add(1)
	return prefix + "_" + base62(n)
}

func base62(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = base62Alphabet[n%62]
		n /= 62
	}
	return string(buf[i:])
}

func nowMS() int64 { return time.Now().UnixMilli() }

// NewStore creates an empty store.
func NewStore() *Store {
	return &Store{
		sessions: make(map[string]*Session),
		messages: make(map[string][]*MessageWithParts),
	}
}

// CreateSession creates a new session with a ses_ id (architecture §2.4).
func (s *Store) CreateSession(parentID, title, directory string) Session {
	now := nowMS()
	sess := &Session{
		ID:        NewID("ses"),
		ParentID:  parentID,
		Title:     title,
		Directory: directory,
		Time:      SessionTime{Created: now, Updated: now},
	}
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.messages[sess.ID] = nil
	s.mu.Unlock()
	return *sess
}

// GetSession returns a copy of the session and whether it exists.
func (s *Store) GetSession(id string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return Session{}, false
	}
	return *sess, true
}

// AppendUserMessage appends a user message built from the prompt text parts and
// returns a copy of the stored MessageWithParts.
func (s *Store) AppendUserMessage(sessionID, messageID string, texts []string) (MessageWithParts, bool) {
	now := nowMS()
	if messageID == "" {
		messageID = NewID("msg")
	}
	completed := now
	mwp := &MessageWithParts{
		Info: Message{
			ID:        messageID,
			Role:      "user",
			SessionID: sessionID,
			Time:      Time{Created: now, Completed: &completed},
		},
	}
	for _, t := range texts {
		mwp.Parts = append(mwp.Parts, Part{
			ID:        NewID("prt"),
			MessageID: messageID,
			SessionID: sessionID,
			Type:      "text",
			Text:      t,
		})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sessionID]; !ok {
		return MessageWithParts{}, false
	}
	s.messages[sessionID] = append(s.messages[sessionID], mwp)
	return copyMessage(mwp), true
}

// NewAssistantMessage creates an assistant message (time.completed=null) and
// appends it. Returns a copy.
func (s *Store) NewAssistantMessage(sessionID string) (MessageWithParts, bool) {
	now := nowMS()
	mwp := &MessageWithParts{
		Info: Message{
			ID:        NewID("msg"),
			Role:      "assistant",
			SessionID: sessionID,
			Time:      Time{Created: now, Completed: nil},
		},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sessionID]; !ok {
		return MessageWithParts{}, false
	}
	s.messages[sessionID] = append(s.messages[sessionID], mwp)
	return copyMessage(mwp), true
}

// AppendTextDelta appends delta text to the assistant message's text part,
// creating the part if needed. Returns a copy of the full part and the message
// id. Field is "text" or "reasoning".
func (s *Store) AppendTextDelta(sessionID, messageID, field, delta string) (Part, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mwp := s.findMessageLocked(sessionID, messageID)
	if mwp == nil {
		return Part{}, false
	}
	partType := "text"
	if field == "reasoning" {
		partType = "reasoning"
	}
	// Find existing part of this type, else create.
	var p *Part
	for i := range mwp.Parts {
		if mwp.Parts[i].Type == partType {
			p = &mwp.Parts[i]
			break
		}
	}
	if p == nil {
		mwp.Parts = append(mwp.Parts, Part{
			ID:        NewID("prt"),
			MessageID: messageID,
			SessionID: sessionID,
			Type:      partType,
		})
		p = &mwp.Parts[len(mwp.Parts)-1]
	}
	p.Text += delta
	return *p, true
}

// CompleteAssistantMessage sets time.completed on the assistant message and
// returns a copy of its info.
func (s *Store) CompleteAssistantMessage(sessionID, messageID string) (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mwp := s.findMessageLocked(sessionID, messageID)
	if mwp == nil {
		return Message{}, false
	}
	now := nowMS()
	mwp.Info.Time.Completed = &now
	return mwp.Info, true
}

// MessageInfo returns a copy of a message's info block.
func (s *Store) MessageInfo(sessionID, messageID string) (Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mwp := s.findMessageLocked(sessionID, messageID)
	if mwp == nil {
		return Message{}, false
	}
	return mwp.Info, true
}

// Messages returns a deep copy of all messages for a session so SSE writers
// cannot race the response (architecture §2.2).
func (s *Store) Messages(sessionID string) ([]MessageWithParts, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msgs, ok := s.messages[sessionID]
	if !ok {
		if _, sessOK := s.sessions[sessionID]; !sessOK {
			return nil, false
		}
	}
	out := make([]MessageWithParts, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, copyMessage(m))
	}
	return out, true
}

// findMessageLocked returns the stored *MessageWithParts; caller holds the lock.
func (s *Store) findMessageLocked(sessionID, messageID string) *MessageWithParts {
	for _, m := range s.messages[sessionID] {
		if m.Info.ID == messageID {
			return m
		}
	}
	return nil
}

func copyMessage(m *MessageWithParts) MessageWithParts {
	out := MessageWithParts{Info: m.Info}
	if m.Info.Time.Completed != nil {
		c := *m.Info.Time.Completed
		out.Info.Time.Completed = &c
	}
	out.Parts = make([]Part, len(m.Parts))
	copy(out.Parts, m.Parts)
	return out
}
