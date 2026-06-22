package queue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/opencode-go/opencode-go/internal/event"
)

// Status constants
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusAborted   = "aborted"
	StatusError     = "error"
)

// Task represents a background queue entry.
type Task struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	Status    string    `json:"status"`             // pending|running|completed|aborted|error
	Artifact  string    `json:"artifact,omitempty"` // path to markdown artifact
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Store persists tasks to a JSON file and provides concurrency safety.
type Store struct {

	// baseDir is the root directory for the queue (tasks.json and artifacts)

	mu       sync.RWMutex
	tasks    map[string]*Task
	baseDir  string // directory containing tasks.json and artifacts subdir
	filePath string // path to tasks.json
}

// NewStore creates a Store loading existing tasks from <baseDir>/tasks.json if present.
func NewStore(baseDir string) *Store {
	s := &Store{
		tasks:    make(map[string]*Task),
		baseDir:  baseDir,
		filePath: filepath.Join(baseDir, "tasks.json"),
	}
	// Ensure directory exists.
	_ = os.MkdirAll(baseDir, 0755)
	s.load()
	return s
}

func (s *Store) load() {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return
	}
	var list []*Task
	if err := json.Unmarshal(data, &list); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range list {
		s.tasks[t.ID] = t
	}
}

// snapshot returns a copy slice of tasks for safe persistence
func (s *Store) snapshot() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		// shallow copy is fine as Task fields are value types
		copyT := *t
		list = append(list, &copyT)
	}
	return list
}

// writeTasks writes the given task list to the JSON file without any locking.
func (s *Store) writeTasks(tasks []*Task) {
	data, _ := json.MarshalIndent(tasks, "", "  ")
	_ = os.WriteFile(s.filePath, data, 0644)
}

// CreateTask adds a new task with the given name and returns it.
func (s *Store) CreateTask(name string) *Task {
	id := event.NewID("task")
	now := time.Now().UTC()
	t := &Task{ID: id, Name: name, Status: StatusQueued, CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	s.tasks[id] = t
	// Unlock before persisting to avoid deadlock
	s.mu.Unlock()
	s.writeTasks(s.snapshot())
	return t
}

// Get returns a task by id.
func (s *Store) Get(id string) (*Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, false
	}
	copyT := *t
	return &copyT, true
}

// UpdateStatus sets the status of a task.
func (s *Store) UpdateStatus(id, status string) bool {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return false
	}
	// If already aborted, do not overwrite
	if t.Status == StatusAborted {
		s.mu.Unlock()
		return false
	}
	t.Status = status
	t.UpdatedAt = time.Now().UTC()
	// Unlock before persisting to avoid deadlock
	s.mu.Unlock()
	s.writeTasks(s.snapshot())
	return true
}

// SetArtifact records the artifact path for a task.
func (s *Store) SetArtifact(id, path string) bool {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return false
	}
	t.Artifact = path
	t.UpdatedAt = time.Now().UTC()
	// Unlock before persisting to avoid deadlock
	s.mu.Unlock()
	s.writeTasks(s.snapshot())
	return true
}

// List returns all tasks.
// BaseDir returns the base directory for the queue store.
func (s *Store) BaseDir() string { return s.baseDir }

func (s *Store) List() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		// copy each task
		copyT := *t
		out = append(out, &copyT)
	}
	return out
}
