package server

import (
    "encoding/json"
    "net/http"
    "time"

    "github.com/opencode-go/opencode-go/internal/event"
    "github.com/opencode-go/opencode-go/internal/session"
)

// DELETE /session/{id}/message/{messageID}
func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
    sessID := r.PathValue("id")
    msgID := r.PathValue("messageID")
    if sessID == "" || msgID == "" {
        writeError(w, http.StatusBadRequest, "missing IDs")
        return
    }
    if _, ok := s.store.GetSession(sessID); !ok {
        writeSessionNotFound(w, sessID)
        return
    }
    if s.sessionBusy(sessID) {
        writeSessionBusy(w, sessID)
        return
    }
    if !s.store.RemoveMessage(sessID, msgID) {
        writeError(w, http.StatusNotFound, "message not found")
        return
    }
    s.bus.Publish(event.NewMessageRemoved(sessID, msgID))
    writeJSON(w, http.StatusOK, true)
}

// DELETE /session/{id}/message/{messageID}/part/{partID}
func (s *Server) handleDeletePart(w http.ResponseWriter, r *http.Request) {
    sessID := r.PathValue("id")
    msgID := r.PathValue("messageID")
    partID := r.PathValue("partID")
    if sessID == "" || msgID == "" || partID == "" {
        writeError(w, http.StatusBadRequest, "missing IDs")
        return
    }
    if _, ok := s.store.GetSession(sessID); !ok {
        writeSessionNotFound(w, sessID)
        return
    }
    if s.sessionBusy(sessID) {
        writeSessionBusy(w, sessID)
        return
    }
    if !s.store.RemovePart(sessID, msgID, partID) {
        writeError(w, http.StatusNotFound, "part not found")
        return
    }
    s.bus.Publish(event.NewPartRemoved(sessID, msgID, partID))
    writeJSON(w, http.StatusOK, true)
}

// PATCH /session/{id}/message/{messageID}/part/{partID}
func (s *Server) handleUpdatePart(w http.ResponseWriter, r *http.Request) {
    sessID := r.PathValue("id")
    msgID := r.PathValue("messageID")
    partID := r.PathValue("partID")
    if sessID == "" || msgID == "" || partID == "" {
        writeError(w, http.StatusBadRequest, "missing IDs")
        return
    }
    if _, ok := s.store.GetSession(sessID); !ok {
        writeSessionNotFound(w, sessID)
        return
    }
    if s.sessionBusy(sessID) {
        writeSessionBusy(w, sessID)
        return
    }
    var newPart session.Part
    if err := json.NewDecoder(r.Body).Decode(&newPart); err != nil {
        writeError(w, http.StatusBadRequest, "invalid JSON body")
        return
    }
    if newPart.ID != "" && newPart.ID != partID {
        writeError(w, http.StatusBadRequest, "part ID mismatch")
        return
    }
    if newPart.MessageID != "" && newPart.MessageID != msgID {
        writeError(w, http.StatusBadRequest, "message ID mismatch")
        return
    }
    updated, ok := s.store.UpdatePart(sessID, msgID, partID, newPart)
    if !ok {
        writeError(w, http.StatusNotFound, "part not found")
        return
    }
    s.bus.Publish(event.NewMessagePartUpdated(sessID, updated, time.Now().UnixMilli()))
    writeJSON(w, http.StatusOK, updated)
}

