package server

import (
	"net/http"

	"github.com/coder/websocket"
)

// handlePtyConnect bridges a websocket byte-stream to an existing PTY session.
func (s *Server) handlePtyConnect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("ptyID")
	p, ok := s.ptys.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "pty not found")
		return
	}
	ticket := r.URL.Query().Get("ticket")
	if !p.RedeemTicket(ticket) {
		writeError(w, http.StatusForbidden, "invalid or expired ticket")
		return
	}
	ptmx := p.Ptmx()
	if ptmx == nil {
		writeError(w, http.StatusGone, "pty closed")
		return
	}
	// InsecureSkipVerify is acceptable: the server binds 127.0.0.1 only
	// (loopback, no-auth posture), so Origin/CORS checks are not needed.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusInternalError, "closing")
	ctx := r.Context()
	// pump 1: ptmx -> websocket (goroutine)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if werr := c.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	// pump 2: websocket -> ptmx (main loop)
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if len(data) > 0 {
			if _, werr := ptmx.Write(data); werr != nil {
				return
			}
		}
	}
}
