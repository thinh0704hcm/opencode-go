package session

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
)

// persistedSession is the on-disk shape: the session plus its full message log.
type persistedSession struct {
	Session  Session            `json:"session"`
	Messages []MessageWithParts `json:"messages"`
}

func (s *Store) sessionsDir() string {
	return filepath.Join(s.persistDir, "sessions")
}

// SetPersistDir enables on-disk persistence rooted at dir and ensures the
// <dir>/sessions directory exists. Empty dir disables persistence (default).
func (s *Store) SetPersistDir(dir string) error {
	if dir != "" {
		if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o755); err != nil {
			return err // persistDir stays "" → persistence genuinely disabled
		}
	}
	s.mu.Lock()
	s.persistDir = dir
	s.mu.Unlock()
	return nil
}

// Load reads every <persistDir>/sessions/*.json into the in-memory maps. A file
// that fails to parse is logged and skipped so one bad file never aborts boot.
// No-op when persistence is disabled.
func (s *Store) Load() error {
	s.mu.RLock()
	dir := s.persistDir
	s.mu.RUnlock()
	if dir == "" {
		return nil
	}
	paths, err := filepath.Glob(filepath.Join(dir, "sessions", "*.json"))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			log.Printf("session: load read %s: %v", p, err)
			continue
		}
		var ps persistedSession
		if err := json.Unmarshal(data, &ps); err != nil {
			log.Printf("session: load parse %s: %v (skipped)", p, err)
			continue
		}
		if ps.Session.ID == "" {
			continue
		}
		sess := ps.Session
		s.sessions[sess.ID] = &sess
		msgs := make([]*MessageWithParts, 0, len(ps.Messages))
		for i := range ps.Messages {
			m := ps.Messages[i]
			msgs = append(msgs, &m)
		}
		// Keep messages ordered by creation time (TUI orders by it).
		sort.SliceStable(msgs, func(i, j int) bool {
			return msgs[i].Info.Time.Created < msgs[j].Info.Time.Created
		})
		// Zombie-close: mark any assistant message that was never completed
		// (time.completed == nil) so the TUI doesn't lock input waiting for a
		// generation that will never finish (server was killed mid-turn).
		nowMs := nowMS()
		for _, m := range msgs {
			if m.Info.Role == "assistant" && m.Info.Time.Completed == nil {
				m.Info.Time.Completed = &nowMs
				m.Info.Finish = "aborted"
			}
		}
		s.messages[sess.ID] = msgs
	}
	return nil
}

// PersistSession writes the session + its messages to disk atomically. It
// marshals UNDER the read lock (mutators mutate stored objects in place, so a
// raw-pointer snapshot would race json.Marshal), then writes the bytes outside
// the lock via a unique same-dir temp file + rename. Best-effort: errors are
// logged, never returned to the caller's turn. No-op when persistence is off.
func (s *Store) PersistSession(sessionID string) {
	s.mu.RLock()
	dir := s.persistDir
	if dir == "" {
		s.mu.RUnlock()
		return
	}
	sess, ok := s.sessions[sessionID]
	if !ok {
		s.mu.RUnlock()
		return
	}
	ps := persistedSession{Session: *sess}
	for _, m := range s.messages[sessionID] {
		ps.Messages = append(ps.Messages, copyMessage(m))
	}
	data, err := json.Marshal(ps)
	s.mu.RUnlock()
	if err != nil {
		log.Printf("session: marshal %s: %v", sessionID, err)
		return
	}
	sessionsDir := filepath.Join(dir, "sessions")
	tmp, err := os.CreateTemp(sessionsDir, sessionID+"-*.tmp")
	if err != nil {
		log.Printf("session: temp %s: %v", sessionID, err)
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		log.Printf("session: write %s: %v", sessionID, err)
		return
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		log.Printf("session: sync %s: %v", sessionID, err)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		log.Printf("session: close %s: %v", sessionID, err)
		return
	}
	final := filepath.Join(sessionsDir, sessionID+".json")
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		log.Printf("session: rename %s: %v", sessionID, err)
	}
}

// RemovePersisted deletes a session's on-disk file. No-op when disabled.
func (s *Store) RemovePersisted(sessionID string) {
	s.mu.RLock()
	dir := s.persistDir
	s.mu.RUnlock()
	if dir == "" {
		return
	}
	if err := os.Remove(filepath.Join(dir, "sessions", sessionID+".json")); err != nil && !os.IsNotExist(err) {
		log.Printf("session: remove %s: %v", sessionID, err)
	}
}
