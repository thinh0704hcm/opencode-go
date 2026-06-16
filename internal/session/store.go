package session

import (
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store is an in-memory session/message store guarded by an RWMutex.
// On-disk persistence is NOT implemented for M1 (architecture §2.2).
type Store struct {
	mu         sync.RWMutex
	sessions   map[string]*Session
	messages   map[string][]*MessageWithParts // ses_* -> ordered messages
	persistDir string                         // when non-empty, sessions are persisted to <persistDir>/sessions/*.json
}

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// ID format mirrors opencode's src/id/id.ts exactly so the TUI's timestamp
// parser stays byte-compatible. The TUI reads the first 12 hex chars after the
// prefix as a big-endian integer and divides by 0x1000 to recover the sort
// timestamp, so the first 6 bytes encode (unixMilli<<12 + counter). Sessions
// are descending (bitwise-NOT), messages/parts/events ascending.
const idTotalLength = 26

var (
	idMu      sync.Mutex
	idLastTS  int64
	idCounter int64
)

// NewID keeps the original signature so all call sites are unchanged. Only
// the "ses" prefix sorts descending; everything else sorts ascending.
func NewID(prefix string) string {
	return createID(prefix, prefix == "ses")
}

func createID(prefix string, descending bool) string {
	idMu.Lock()
	ts := time.Now().UnixMilli()
	if ts != idLastTS {
		idLastTS = ts
		idCounter = 0
	}
	idCounter++
	c := idCounter
	idMu.Unlock()

	v := uint64(ts)*0x1000 + uint64(c) // timestamp<<12 + counter
	if descending {
		v = ^v // bitwise NOT; only the low 48 bits are emitted below
	}
	b := make([]byte, 6)
	for i := 0; i < 6; i++ {
		b[i] = byte((v >> uint(40-8*i)) & 0xff)
	}
	hexpart := hex.EncodeToString(b) // 12 hex chars
	return prefix + "_" + hexpart + randomBase62(idTotalLength-12)
}

func randomBase62(n int) string {
	bs := make([]byte, n)
	_, _ = rand.Read(bs)
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = base62Alphabet[int(bs[i])%62]
	}
	return string(out)
}

func nowMS() int64 { return time.Now().UnixMilli() }

var (
	monoMu sync.Mutex
	lastMS int64
)

// nextMS returns a strictly-increasing epoch-ms timestamp, so no two
// messages created in the same millisecond collide (the TUI orders by it).
func nextMS() int64 {
	monoMu.Lock()
	defer monoMu.Unlock()
	t := nowMS()
	if t <= lastMS {
		t = lastMS + 1
	}
	lastMS = t
	return t
}

// NewStore creates an empty store.
func NewStore() *Store {
	return &Store{
		sessions: make(map[string]*Session),
		messages: make(map[string][]*MessageWithParts),
	}
}

func sessionTitleFromTexts(texts []string) string {
	joined := strings.TrimSpace(strings.Join(texts, " "))
	joined = strings.Join(strings.Fields(joined), " ")
	if joined == "" {
		return "New session"
	}
	r := []rune(joined)
	if len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return joined
}

// CreateSession creates a new session with a ses_ id (architecture §2.4).
func (s *Store) CreateSession(parentID, title, directory string) Session {
	return s.CreateSessionWithID("", parentID, title, directory)
}

// CreateSessionWithID behaves like CreateSession but uses the caller-supplied id
// when non-empty, falling back to a generated ID otherwise.
func (s *Store) CreateSessionWithID(id, parentID, title, directory string) Session {
	now := nowMS()
	if id == "" {
		id = NewID("ses")
	}
	slug := id
	if strings.HasPrefix(slug, "ses_") {
		slug = slug[4:]
	}
	if len(slug) > 8 {
		slug = slug[:8]
	}
	sess := &Session{
		ID:        id,
		Slug:      slug,
		ParentID:  parentID,
		Title:     title,
		Directory: directory,
		Time:      SessionTime{Created: now, Updated: now},
		Version:   "1.17.4",
		ProjectID: func() string {
			if directory == "" {
				return "default"
			}
			return filepath.Base(directory)
		}(),
	}
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.messages[sess.ID] = nil
	s.mu.Unlock()
	return *sess
}

// DropTextAndReasoningParts removes all text and reasoning parts from an
// assistant message. Called before a stream retry so partial content from the
// failed attempt doesn't duplicate in the next attempt.
func (s *Store) DropTextAndReasoningParts(sessionID, messageID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mwp := s.findMessageLocked(sessionID, messageID)
	if mwp == nil {
		return
	}
	kept := mwp.Parts[:0]
	for _, p := range mwp.Parts {
		if p.Type != "text" && p.Type != "reasoning" {
			kept = append(kept, p)
		}
	}
	mwp.Parts = kept
}

// PersistAll persists every session to disk. Called at graceful shutdown so no
// conversation data is lost on restart.
func (s *Store) PersistAll() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		s.PersistSession(id)
	}
}

// List returns a snapshot copy of all sessions sorted by Time.Created
// (ascending). Callers receive value copies, so the slice is safe to mutate.
func (s *Store) List() []Session {
	s.mu.RLock()
	out := make([]Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, *sess)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].Time.Created < out[j].Time.Created
	})
	return out
}

// Children returns all sessions whose ParentID equals parentID, sorted by creation time.
func (s *Store) Children(parentID string) []Session {
	s.mu.RLock()
	out := make([]Session, 0)
	for _, sess := range s.sessions {
		if sess.ParentID == parentID {
			out = append(out, *sess)
		}
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].Time.Created < out[j].Time.Created
	})
	return out
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

// GetSessionChildren returns all child sessions of a parent.
func (s *Store) GetSessionChildren(parentID string) []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var children []Session
	for _, ses := range s.sessions {
		if ses.ParentID == parentID {
			children = append(children, *ses)
		}
	}
	// Bubble sort for simplicity since list is usually small
	for i := 0; i < len(children)-1; i++ {
		for j := i + 1; j < len(children); j++ {
			if children[i].Time.Created > children[j].Time.Created {
				children[i], children[j] = children[j], children[i]
			}
		}
	}
	return children
}

// UpdateTitle updates the session and bumps Time.Updated, returning a copy of
// the updated session and whether it existed. A nil title leaves the existing
// title unchanged (the PATCH field was omitted); a non-nil title is applied.
func (s *Store) UpdateTitle(id string, title *string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return Session{}, false
	}
	if title != nil {
		sess.Title = *title
	}
	sess.Time.Updated = nowMS()
	s.sessions[id] = sess
	return *sess, true
}

func (s *Store) UpdateSessionTitle(id, title string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return false
	}
	sess.Title = title
	sess.Time.Updated = nowMS()
	s.sessions[id] = sess
	return true
}

// Delete removes a session and its messages. Returns whether it existed.
func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return false
	}
	delete(s.sessions, id)
	delete(s.messages, id)
	return true
}

// GetMessage returns a deep copy of a single message's {info, parts} and whether
// it exists, matching Messages() deep-copy-on-read semantics (architecture §2.2).
func (s *Store) GetMessage(sessionID, messageID string) (MessageWithParts, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mwp := s.findMessageLocked(sessionID, messageID)
	if mwp == nil {
		return MessageWithParts{}, false
	}
	return copyMessage(mwp), true
}

// AppendUserMessage appends a user message built from the prompt text parts and
// returns a copy of the stored MessageWithParts.
func (s *Store) AppendUserMessage(sessionID, messageID, providerID, modelID, agentName string, texts []string) (MessageWithParts, bool) {
	now := nextMS()
	if messageID == "" {
		messageID = NewID("msg")
	}
	if agentName == "" {
		agentName = "build"
	}
	mwp := &MessageWithParts{
		Info: Message{
			ID:        messageID,
			Role:      "user",
			SessionID: sessionID,
			Time:      Time{Created: now},
			Agent:     agentName,
			Summary:   &MsgSummary{Diffs: []any{}},
		},
	}
	if providerID != "" && modelID != "" {
		mwp.Info.Model = &MsgModel{ProviderID: providerID, ModelID: modelID}
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
	if sess := s.sessions[sessionID]; sess != nil && strings.TrimSpace(sess.Title) == "" {
		sess.Title = sessionTitleFromTexts(texts)
		sess.Time.Updated = nowMS()
	}
	return copyMessage(mwp), true
}

// RemoveMessage removes a message and its parts from a session.
func (s *Store) RemoveMessage(sessionID, messageID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs, ok := s.messages[sessionID]
	if !ok {
		return false
	}
	for i, m := range msgs {
		if m.Info.ID == messageID {
			s.messages[sessionID] = append(msgs[:i], msgs[i+1:]...)
			return true
		}
	}
	return false
}

// NewAssistantMessage creates an assistant message (time.completed=null) and
// appends it. Returns a copy.
func (s *Store) NewAssistantMessage(sessionID, parentID, providerID, modelID, agentName, mode string, hidden ...bool) (MessageWithParts, bool) {
	isHidden := len(hidden) > 0 && hidden[0]
	now := nextMS()
	if agentName == "" {
		agentName = "build"
	}
	if mode == "" {
		mode = "build"
	}
	zeroCost := 0.0
	mwp := &MessageWithParts{
		Info: Message{
			ID:         NewID("msg"),
			Role:       "assistant",
			SessionID:  sessionID,
			Time:       Time{Created: now, Completed: nil},
			Agent:      agentName,
			ParentID:   parentID,
			ProviderID: providerID,
			ModelID:    modelID,
			Mode:       mode,
			Finish:     "stop",
			Cost:       &zeroCost,
			Hidden:     isHidden,
			// Non-nil so the TUI can read tokens.output without dereferencing nil.
			Tokens: &Tokens{Input: 0, Output: 0, Reasoning: 0, Cache: TokenCache{Read: 0, Write: 0}},
			Path:   &MsgPath{Cwd: ".", Root: "."},
		},
	}
	// Real opencode assistant messages begin with a step-start part; the text
	// part(s) and a trailing step-finish part follow during the turn.
	mwp.Parts = append(mwp.Parts, Part{
		ID:        NewID("prt"),
		MessageID: mwp.Info.ID,
		SessionID: sessionID,
		Type:      "step-start",
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sessionID]; !ok {
		return MessageWithParts{}, false
	}
	s.messages[sessionID] = append(s.messages[sessionID], mwp)
	return copyMessage(mwp), true
}

// CopyMessage deep-copies a message and its parts into a target session with new IDs.
func (s *Store) CopyMessage(targetSessionID string, m MessageWithParts) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[targetSessionID]; !ok {
		return
	}

	newMsg := copyMessage(&m)
	newMsg.Info.ID = NewID("msg")
	newMsg.Info.SessionID = targetSessionID
	for i := range newMsg.Parts {
		newMsg.Parts[i].ID = NewID("prt")
		newMsg.Parts[i].MessageID = newMsg.Info.ID
		newMsg.Parts[i].SessionID = targetSessionID
	}
	s.messages[targetSessionID] = append(s.messages[targetSessionID], &newMsg)
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
			// Assistant text parts carry a start time (user text parts do not).
			Time: &PartTime{Start: nextMS()},
		})
		p = &mwp.Parts[len(mwp.Parts)-1]
	}
	p.Text += delta
	return copyPart(*p), true
}

func toolDisplay(toolName string, input map[string]any, output string) (string, map[string]any) {
	title := ""
	desc := ""
	if toolName == "task" || toolName == "delegate" {
		agent := "general"
		if input != nil {
			if v, ok := input["agent"].(string); ok && strings.TrimSpace(v) != "" {
				agent = strings.TrimSpace(v)
			}
			if v, ok := input["description"].(string); ok && strings.TrimSpace(v) != "" {
				desc = strings.TrimSpace(v)
			} else if v, ok := input["prompt"].(string); ok && strings.TrimSpace(v) != "" {
				desc = strings.TrimSpace(v)
			}
		}
		title = agent + " " + toolName
	} else {
		title = toolName
	}
	return title, map[string]any{"output": output, "description": desc}
}

// UpdateSubtaskTarget updates the TargetSessionID of a subtask part.
// Returns a copy of the updated part and whether the message/part was found.
func (s *Store) UpdateSubtaskTarget(sessionID, messageID, prompt, targetSessionID string) (Part, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mwp := s.findMessageLocked(sessionID, messageID)
	if mwp == nil {
		return Part{}, false
	}
	
	for i := len(mwp.Parts)-1; i >= 0; i-- {
		if mwp.Parts[i].Type == "subtask" && mwp.Parts[i].Prompt == prompt {
			mwp.Parts[i].TargetSessionID = targetSessionID
			return mwp.Parts[i], true
		}
	}
	return Part{}, false
}

// AppendSubtaskPart records a subtask delegation part on the assistant message.
// Returns a copy of the new part and whether the message was found.
func (s *Store) AppendSubtaskPart(sessionID, messageID, prompt, desc, agentName, providerID, modelID, targetSessionID string) (Part, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mwp := s.findMessageLocked(sessionID, messageID)
	if mwp == nil {
		return Part{}, false
	}

	part := Part{
		ID:              NewID("prt"),
		MessageID:       messageID,
		SessionID:       sessionID,
		Type:            "subtask",
		Prompt:          prompt,
		Description:     desc,
		Agent:           agentName,
		TargetSessionID: targetSessionID,
	}
	if providerID != "" || modelID != "" {
		part.Model = &PartModel{
			ProviderID: providerID,
			ModelID:    modelID,
		}
	}

	mwp.Parts = append(mwp.Parts, part)
	return part, true
}

// AppendToolPart records a tool-activity part on the assistant message so the
// agent loop can surface tool calls. Returns a copy of the new part and whether
// the message was found.
func (s *Store) AppendToolPart(sessionID, messageID, toolName, callID, status string, input map[string]any, output string) (Part, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mwp := s.findMessageLocked(sessionID, messageID)
	if mwp == nil {
		return Part{}, false
	}
	now := nextMS()
	var end *int64
	if status == "completed" || status == "error" {
		v := now + 1
		end = &v
	}
	title, metadata := toolDisplay(toolName, input, output)
	// Upsert by callID: the agent loop calls this twice per tool ("running"
	// then "completed"/"error"). Update the same part in place so the TUI sees
	// one part transition rather than an orphaned spinner. Empty callID always
	// appends to avoid collapsing unrelated parts.
	if callID != "" {
		for i := range mwp.Parts {
			ep := &mwp.Parts[i]
			if ep.Type != "tool" || ep.CallID != callID {
				continue
			}
			if ep.State == nil {
				ep.State = &PartState{}
			}
			if ep.State.Time == nil {
				ep.State.Time = &PartStateTime{Start: now}
			}
			ep.State.Status = status
			if input != nil {
				ep.State.Input = input
			}
			if status == "error" {
				ep.State.Error = output
				ep.State.Output = ""
			} else {
				ep.State.Output = output
				ep.State.Error = ""
			}
			ep.State.Title = title
			ep.State.Metadata = metadata
			if status == "completed" && ep.State.Metadata == nil {
				ep.State.Metadata = map[string]any{}
			}
			if end != nil {
				if ep.State.Time != nil && *end <= ep.State.Time.Start {
					v := ep.State.Time.Start + 1
					end = &v
				}
				ep.State.Time.End = end
			}
			return copyPart(*ep), true
		}
	}

	state := &PartState{
		Status:   status,
		Input:    input,
		Title:    title,
		Metadata: metadata,
		Time:     &PartStateTime{Start: now, End: end},
	}
	if status == "error" {
		state.Error = output
	} else {
		state.Output = output
	}
	if status == "completed" && state.Metadata == nil {
		state.Metadata = map[string]any{}
	}

	mwp.Parts = append(mwp.Parts, Part{
		ID:        NewID("prt"),
		MessageID: messageID,
		SessionID: sessionID,
		Type:      "tool",
		Tool:      toolName,
		CallID:    callID,
		State:     state,
	})
	p := &mwp.Parts[len(mwp.Parts)-1]
	return copyPart(*p), true
}

// AppendStepFinish appends a step-finish part (carrying reason/cost/tokens) to
// the assistant message, mirroring real opencode's terminal part. Returns a
// copy of the new part and whether the message was found.
func (s *Store) AppendStepFinish(sessionID, messageID, reason string, cost float64, tokens *Tokens) (Part, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mwp := s.findMessageLocked(sessionID, messageID)
	if mwp == nil {
		return Part{}, false
	}
	mwp.Parts = append(mwp.Parts, Part{
		ID:        NewID("prt"),
		MessageID: messageID,
		SessionID: sessionID,
		Type:      "step-finish",
		Reason:    reason,
		Cost:      &cost,
		Tokens:    tokens,
	})
	p := &mwp.Parts[len(mwp.Parts)-1]
	return copyPart(*p), true
}

// SetAssistantUsage sets the token accounting on an assistant message's info
// from a provider usage object. Reasoning and cache counts stay 0 (the
// OpenAI-compatible stream does not break those out). No-op if missing.
func (s *Store) SetAssistantUsage(sessionID, messageID string, input, output, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mwp := s.findMessageLocked(sessionID, messageID)
	if mwp == nil {
		return
	}
	mwp.Info.Tokens = &Tokens{
		Total:     int64(total),
		Input:     int64(input),
		Output:    int64(output),
		Reasoning: 0,
		Cache:     TokenCache{Read: 0, Write: 0},
	}
}

// returns a copy of its info.
func (s *Store) CompleteAssistantMessage(sessionID, messageID string) (Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mwp := s.findMessageLocked(sessionID, messageID)
	if mwp == nil {
		return Message{}, false
	}
	now := nextMS()
	mwp.Info.Time.Completed = &now
	// Bump session updated timestamp so sort-by-recent works in the TUI.
	if sess := s.sessions[sessionID]; sess != nil {
		sess.Time.Updated = now
	}
	return mwp.Info, true
}

// FinishOpenParts closes assistant streaming parts by setting Time.End. Both
// text and reasoning parts must be closed; otherwise the attach TUI keeps the
// turn in a perpetual "Thinking" state even after the assistant message is
// completed. Returns updated part snapshots so callers can publish them.
func (s *Store) FinishOpenParts(sessionID, messageID string) []Part {
	s.mu.Lock()
	defer s.mu.Unlock()
	mwp := s.findMessageLocked(sessionID, messageID)
	if mwp == nil {
		return nil
	}
	updated := []Part{}
	for i := range mwp.Parts {
		p := &mwp.Parts[i]
		if (p.Type == "text" || p.Type == "reasoning") && p.Time != nil && p.Time.End == nil {
			end := nextMS()
			p.Time.End = &end
			updated = append(updated, copyPart(*p))
		}
	}
	return updated
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
	if m.Info.Tokens != nil {
		t := *m.Info.Tokens
		out.Info.Tokens = &t
	}
	if m.Info.Path != nil {
		p := *m.Info.Path
		out.Info.Path = &p
	}
	if m.Info.Cost != nil {
		c := *m.Info.Cost
		out.Info.Cost = &c
	}
	out.Parts = make([]Part, len(m.Parts))
	for i := range m.Parts {
		out.Parts[i] = copyPart(m.Parts[i])
	}
	return out
}

// copyPart returns a value copy of a Part with cloned pointer fields (State,
// Time, Tokens) so concurrent readers never share mutable pointers.
func copyPart(p Part) Part {
	if p.State != nil {
		st := *p.State
		if p.State.Time != nil {
			t := *p.State.Time
			st.Time = &t
		}
		p.State = &st
	}
	if p.Time != nil {
		t := *p.Time
		if p.Time.End != nil {
			e := *p.Time.End
			t.End = &e
		}
		p.Time = &t
	}
	if p.Tokens != nil {
		tk := *p.Tokens
		p.Tokens = &tk
	}
	if p.Cost != nil {
		c := *p.Cost
		p.Cost = &c
	}
	return p
}
