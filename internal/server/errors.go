package server

import (
	"encoding/json"
	"net/http"
)

// errorEnvelope is the canonical JSON error body {error:{message}}
// (architecture §1 errors.go).
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Message string `json:"message"`
}

// writeJSON writes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes the canonical error envelope.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorEnvelope{Error: errorBody{Message: msg}})
}

// writeTaggedError writes an error JSON with a custom _tag and additional fields.
func writeTaggedError(w http.ResponseWriter, status int, tag string, data map[string]any) {
    body := map[string]any{"_tag": tag}
    for k, v := range data {
        body[k] = v
    }
    writeJSON(w, status, body)
}

// writeSessionBusy writes a 409 Conflict error with SessionBusyError tag.
func writeSessionBusy(w http.ResponseWriter, sessionID string) {
    writeTaggedError(w, http.StatusConflict, "SessionBusyError", map[string]any{
        "sessionID": sessionID,
        "message":   "Session is busy: " + sessionID,
    })
}

// writeSessionNotFound writes a 404 Not Found error with SessionNotFoundError tag.
func writeSessionNotFound(w http.ResponseWriter, sessionID string) {
    writeTaggedError(w, http.StatusNotFound, "SessionNotFoundError", map[string]any{
        "sessionID": sessionID,
        "message":   "session not found",
    })
}
