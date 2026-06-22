package server

import (
	"net/http"

	"github.com/opencode-go/opencode-go/internal/permission"
)

// permissionReplyRequest is the body for POST /permission/{requestID}/reply.
type permissionReplyRequest struct {
	Reply string `json:"reply"`
}

// handlePermissionReply serves POST /permission/{requestID}/reply (bot primary).
// Body {"reply":"once|always|reject"}. Unknown id -> 404 (architecture §4.2/B2).
func (s *Server) handlePermissionReply(w http.ResponseWriter, r *http.Request) {
	requestID := r.PathValue("requestID")
	var req permissionReplyRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s.replyPermission(w, requestID, req.Reply)
}

// permissionRespondRequest is the body for the fallback endpoint.
type permissionRespondRequest struct {
	Response string `json:"response"`
}

// handlePermissionRespond serves POST /session/{sessionID}/permissions/{permissionID}
// (bot fallback / spec canonical). Body {"response":"once|always|reject"}.
// Wired to the same gate. Unknown id -> 404 (architecture §4.2/B2).
func (s *Server) handlePermissionRespond(w http.ResponseWriter, r *http.Request) {
	permissionID := r.PathValue("permissionID")
	var req permissionRespondRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s.replyPermission(w, permissionID, req.Response)
}

// replyPermission resolves a pending permission request and publishes the
// permission.replied event. Both reply endpoints feed this one gate.
func (s *Server) replyPermission(w http.ResponseWriter, requestID, reply string) {
	sessionID := s.perms.SessionID(requestID)
	// validate reply
	switch reply {
	case "once", "always", "reject":
		// ok
	default:
		writeError(w, http.StatusBadRequest, "invalid reply")
		return
	}
	if err := s.perms.Reply(requestID, reply); err != nil {
		if err == permission.ErrUnknown {
			writeError(w, http.StatusNotFound, "unknown permission request")
			return
		}
		writeError(w, http.StatusInternalServerError, "permission reply failed")
		return
	}
	s.publishPermissionReplied(sessionID, requestID, reply)
	w.WriteHeader(http.StatusOK)
}
