package server

import (
	"encoding/json"
	"net/http"

	"github.com/opencode-go/opencode-go/internal/pty"
	"github.com/opencode-go/opencode-go/internal/session"
)

// handlePtyList handles GET /pty and returns the list of active ptys.
func (s *Server) handlePtyList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ptys.List())
}

// handlePtyShells handles GET /pty/shells and returns available shells.
func (s *Server) handlePtyShells(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, pty.Shells())
}

// handlePtyCreate handles POST /pty and creates a new pty.
func (s *Server) handlePtyCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title   string `json:"title"`
		Command string `json:"command"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	cwd := directoryParam(r)
	id := session.NewID("pty")
	p, err := s.ptys.Create(id, body.Title, body.Command, cwd)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p.Info())
}

// handlePtyGet handles GET /pty/{ptyID}.
func (s *Server) handlePtyGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("ptyID")
	p, ok := s.ptys.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "pty not found")
		return
	}
	writeJSON(w, http.StatusOK, p.Info())
}

// handlePtyUpdate handles PUT /pty/{ptyID} and resizes the pty.
func (s *Server) handlePtyUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("ptyID")
	p, ok := s.ptys.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "pty not found")
		return
	}
	var body struct {
		Rows uint16 `json:"rows"`
		Cols uint16 `json:"cols"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if err := p.Resize(body.Rows, body.Cols); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p.Info())
}

// handlePtyRemove handles DELETE /pty/{ptyID}.
func (s *Server) handlePtyRemove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("ptyID")
	ok := s.ptys.Remove(id)
	writeJSON(w, http.StatusOK, ok)
}

// handlePtyConnectToken handles POST /pty/{ptyID}/connect-token and issues a
// single-use ticket for the websocket connection.
func (s *Server) handlePtyConnectToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("ptyID")
	p, ok := s.ptys.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "pty not found")
		return
	}
	ticket := p.IssueTicket()
	writeJSON(w, http.StatusOK, map[string]string{"ticket": ticket, "token": ticket})
}
