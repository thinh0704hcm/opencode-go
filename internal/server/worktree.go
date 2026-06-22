//go:build opencode_wip

package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/opencode-go/opencode-go/internal/session"
)

// sanitizePath returns cleaned absolute path with symlinks resolved.
func sanitizePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	// Resolve symlinks; if fails, use abs.
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		real = abs
	}
	return real, nil
}

// Worktree represents a git worktree managed by the experimental API.
// ID is a deterministic identifier derived from the absolute path.
type Worktree struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	SessionID string `json:"session,omitempty"`
}

// WorktreeRegistry holds the in‑memory map of worktrees and persists it to
// $WORKDIR/.opencode-go/worktrees.json.
type WorktreeRegistry struct {
	mu   sync.RWMutex
	data map[string]*Worktree // keyed by ID (same as Path for simplicity)
	file string               // absolute path to JSON persistence file
}

// NewWorktreeRegistry creates a registry. If dataDir is non‑empty it is used as
// the base for the JSON file; otherwise $WORKDIR/.opencode-go is used.
func NewWorktreeRegistry(workdir, dataDir string) *WorktreeRegistry {
	var dir string
	if dataDir != "" {
		dir = dataDir
	} else {
		dir = filepath.Join(workdir, ".opencode-go")
	}
	// Ensure directory exists – ignore errors, they will surface on write.
	_ = os.MkdirAll(dir, 0o755)
	file := filepath.Join(dir, "worktrees.json")
	reg := &WorktreeRegistry{data: map[string]*Worktree{}, file: file}
	reg.load()
	return reg
}

// load reads the persisted JSON file if present.
func (r *WorktreeRegistry) load() {
	b, err := os.ReadFile(r.file)
	if err != nil {
		// No file is fine – start empty.
		return
	}
	var list []Worktree
	if err := json.Unmarshal(b, &list); err != nil {
		// Corrupt file – start fresh.
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range list {
		wt := list[i]
		r.data[wt.ID] = &wt
	}
}

// save writes the current in‑memory map to disk.
func (r *WorktreeRegistry) save() error {
	// Caller must hold appropriate lock (RLock for read-only, Lock for write).
	list := make([]Worktree, 0, len(r.data))
	for _, wt := range r.data {
		list = append(list, *wt)
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.file, b, 0o644)
}

// List returns a slice of all registered worktrees.
func (r *WorktreeRegistry) List() []Worktree {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Worktree, 0, len(r.data))
	for _, wt := range r.data {
		out = append(out, *wt)
	}
	return out
}

// Add registers a new worktree. The path must be absolute and reside under the
// user’s opencode worktree directory (~/\.local/share/opencode/worktrees).
func (r *WorktreeRegistry) Add(path string) (*Worktree, error) {
	// Resolve and sanitize path
	abs, err := sanitizePath(path)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(home, ".local", "share", "opencode", "worktrees")
	// Ensure path is within allowed base directory using safe containment.
	// Resolve and sanitize base directory.
	baseAbs, err := sanitizePath(base)
	if err != nil {
		return nil, err
	}
	// Compute relative path from base to target.
	rel, err := filepath.Rel(baseAbs, abs)
	if err != nil {
		return nil, err
	}
	// If rel starts with ".." or is absolute, target is outside base.
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return nil, errors.New("worktree path outside allowed directory")
	}
	id := abs // deterministic ID
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.data[id]; exists {
		return nil, errors.New("worktree already exists")
	}
	wt := &Worktree{ID: id, Path: abs}
	r.data[id] = wt
	if err := r.save(); err != nil {
		// rollback registration on failure
		delete(r.data, id)
		return nil, err
	}
	return wt, nil
}

// Delete removes a worktree from the registry.
func (r *WorktreeRegistry) Delete(path string, sessStore *session.Store) error {
	// Resolve and sanitize path
	abs, err := sanitizePath(path)
	if err != nil {
		return err
	}
	id := abs
	r.mu.Lock()
	wt, ok := r.data[id]
	if !ok {
		r.mu.Unlock()
		return errors.New("worktree not found")
	}
	// If the worktree had an associated session, clear its directory.
	if wt.SessionID != "" && sessStore != nil {
		sessStore.UpdateSessionDirectory(wt.SessionID, "")
		sessStore.PersistSession(wt.SessionID)
	}
	delete(r.data, id)
	err = r.save()
	r.mu.Unlock()
	return err
}

// Assign links a session to a worktree, updating the session directory via the
// underlying store. It also records the assignment in the registry.
func (r *WorktreeRegistry) Assign(sessStore *session.Store, sessionID, path string) error {
	// Resolve and sanitize path
	abs, err := sanitizePath(path)
	if err != nil {
		return err
	}
	r.mu.Lock()
	wt, ok := r.data[abs]
	if !ok {
		r.mu.Unlock()
		return errors.New("worktree not found")
	}
	// Update session directory.
	if !sessStore.UpdateSessionDirectory(sessionID, abs) {
		r.mu.Unlock()
		return errors.New("session not found")
	}
	wt.SessionID = sessionID
	// Persist registry while lock held to avoid race.
	err = r.save()
	// Unlock before persisting session to avoid deadlock with store lock.
	r.mu.Unlock()
	if err != nil {
		return err
	}
	// Persist session changes.
	sessStore.PersistSession(sessionID)
	return nil
}

// Reset clears a session's assigned worktree.
func (r *WorktreeRegistry) Reset(sessStore *session.Store, sessionID string) error {
	// Lock registry to modify.
	r.mu.Lock()
	// Find any worktree pointing to this session.
	for _, wt := range r.data {
		if wt.SessionID == sessionID {
			wt.SessionID = ""
		}
	}
	// Clear session directory.
	if !sessStore.UpdateSessionDirectory(sessionID, "") {
		r.mu.Unlock()
		return errors.New("session not found")
	}
	// Persist registry while lock held.
	err := r.save()
	// Unlock before persisting session.
	r.mu.Unlock()
	if err != nil {
		return err
	}
	sessStore.PersistSession(sessionID)
	return nil
}
