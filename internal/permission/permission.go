package permission

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
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
	allowed  map[string]bool     // sessionID\x00toolName -> always-allowed
	path     string
}

// NewStore creates an empty permission store.
func NewStore() *Store { return NewStoreWithPath("") }

// NewStoreWithPath creates a store and loads persisted grants from the given path.
func NewStoreWithPath(path string) *Store {
	s := &Store{requests: make(map[string]*Request), allowed: make(map[string]bool), path: path}
	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			var loaded map[string]bool
			if json.Unmarshal(data, &loaded) == nil {
				s.allowed = loaded
			}
		}
	}
	return s
}

// Allow records a per-session, per-tool "always" grant so subsequent calls of
// that tool in the same session skip the permission gate.
func (s *Store) Allow(sessionID, tool string) {
	s.mu.Lock()
	s.allowed[sessionID+"\x00"+tool] = true
	tmp := make(map[string]bool, len(s.allowed))
	for k, v := range s.allowed {
		tmp[k] = v
	}
	path := s.path
	s.mu.Unlock()
	if path != "" {
		data, err := json.Marshal(tmp)
		if err == nil {
			// atomic write via temp file
			tmpPath := path + ".tmp"
			_ = os.WriteFile(tmpPath, data, 0o644)
			_ = os.Rename(tmpPath, path)
		}
	}
}

// IsAllowed reports whether the tool was previously always-allowed for the session.
func (s *Store) IsAllowed(sessionID, tool string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.allowed[sessionID+"\x00"+tool]
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

// Ask registers a new pending request (creating its reply channel) and returns it.
func (s *Store) Ask(id, sessionID, permissionName string) *Request {
	req := &Request{ID: id, SessionID: sessionID, Permission: permissionName, replyCh: make(chan string, 1)}
	s.Register(req)
	return req
}

// Wait blocks until the request is replied to, the timeout elapses, or ctx is
// cancelled. On timeout or cancellation it removes the pending request and
// returns "reject" (default-deny). Returns the normalized reply otherwise.
func (s *Store) Wait(ctx context.Context, req *Request, timeout time.Duration) string {
	select {
	case r := <-req.replyCh:
		return normalizeReply(r)
	case <-time.After(timeout):
		s.remove(req.ID)
		return "reject"
	case <-ctx.Done():
		s.remove(req.ID)
		return "reject"
	}
}

func (s *Store) remove(id string) {
	s.mu.Lock()
	delete(s.requests, id)
	s.mu.Unlock()
}

func normalizeReply(r string) string {
	switch r {
	case "allow":
		return "once"
	case "once", "always", "reject":
		return r
	default:
		return "reject"
	}
}
