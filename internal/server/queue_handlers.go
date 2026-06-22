//go:build opencode_wip

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	_ "path/filepath"
)

// handleQueueCreate creates a new background task.
func (s *Server) handleQueueCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if err := json.Unmarshal(body, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	task := s.queueStore.CreateTask(req.Name)
	writeJSON(w, http.StatusOK, task)
}

// handleQueueStatus returns the status of a task.
func (s *Server) handleQueueStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	task, ok := s.queueStore.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

// handleQueueArtifact returns the markdown artifact for a completed task.
func (s *Server) handleQueueArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	task, ok := s.queueStore.Get(id)
	if !ok || task.Artifact == "" {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	data, err := os.ReadFile(task.Artifact)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read artifact")
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleQueueAbort aborts a running task.
func (s *Server) handleQueueAbort(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	ok := s.queueStore.UpdateStatus(id, "aborted")
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, true)
}
