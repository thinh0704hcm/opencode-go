package permission

import (
	"errors"
	"sync"
)

// ErrUnknown is returned when a permission/request id is not pending.
var ErrUnknown = errors.New("unknown permission request")

// Request is a pending permission request (architecture §4.2). For M1 there is
// no real tool loop, so requests are only created by future tool execution; the
// store + reply endpoints exist and are wired to one gate.
type Request struct {
	ID         string `json:"id"` // per_*
	SessionID  string `json:"sessionID"`
	Permission string `json:"permission"`

	replyCh chan string
}

// Store holds pending permission requests, guarded by a mutex. Both reply
// endpoints feed the same reply channel (architecture §4.2 / B2).
type Store struct {
	mu       sync.Mutex
	requests map[string]*Request // requestID -> request
}

// NewStore creates an empty permission store.
func NewStore() *Store {
	return &Store{requests: make(map[string]*Request)}
}

// Register stores a request and creates its reply channel.
func (s *Store) Register(req *Request) {
	if req.replyCh == nil {
		req.replyCh = make(chan string, 1)
	}
	s.mu.Lock()
	s.requests[req.ID] = req
	s.mu.Unlock()
}

// Reply normalizes the reply, sends it to the waiting request, and removes it
// from the store. Returns ErrUnknown if the id is not pending (so callers can
// 404). reply is one of once|always|reject.
func (s *Store) Reply(requestID, reply string) error {
	s.mu.Lock()
	req, ok := s.requests[requestID]
	if ok {
		delete(s.requests, requestID)
	}
	s.mu.Unlock()
	if !ok {
		return ErrUnknown
	}
	select {
	case req.replyCh <- reply:
	default:
	}
	return nil
}

// SessionID returns the session id for a (now-removed or still-pending) request
// id, used to build the permission.replied event. Returns "" if unknown.
func (s *Store) SessionID(requestID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req, ok := s.requests[requestID]; ok {
		return req.SessionID
	}
	return ""
}

// List returns a snapshot of pending requests.
func (s *Store) List() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Request, 0, len(s.requests))
	for _, r := range s.requests {
		out = append(out, Request{ID: r.ID, SessionID: r.SessionID, Permission: r.Permission})
	}
	return out
}
