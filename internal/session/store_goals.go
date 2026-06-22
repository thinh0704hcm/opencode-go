package session

// GetGoals returns the goal list for a session.
func (s *Store) GetGoals(sessionID string) ([]Goal, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.goals[sessionID]
	if !ok {
		return []Goal{}, false
	}
	cp := make([]Goal, len(g))
	copy(cp, g)
	return cp, true
}

// SetGoals replaces the goal list for a session.
func (s *Store) SetGoals(sessionID string, goals []Goal) {
	s.mu.Lock()
	if s.goals == nil {
		s.goals = make(map[string][]Goal)
	}
	cp := make([]Goal, len(goals))
	copy(cp, goals)
	s.goals[sessionID] = cp
	s.mu.Unlock()
	// persist session after updating goals
	s.PersistSession(sessionID)
}
