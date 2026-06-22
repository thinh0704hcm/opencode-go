package session

// GetTodos returns the todo list for a session.
func (s *Store) GetTodos(sessionID string) ([]Todo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.todos[sessionID]
	if !ok {
		return []Todo{}, false
	}
	cp := make([]Todo, len(t))
	copy(cp, t)
	return cp, true
}

// SetTodos replaces the todo list for a session.
func (s *Store) SetTodos(sessionID string, todos []Todo) {
	s.mu.Lock()
	if s.todos == nil {
		s.todos = make(map[string][]Todo)
	}
	cp := make([]Todo, len(todos))
	copy(cp, todos)
	s.todos[sessionID] = cp
	s.mu.Unlock()
	// persist session after updating todos
	s.PersistSession(sessionID)
}
