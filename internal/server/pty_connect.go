package server

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
)

// handlePtyConnect bridges a websocket to an existing PTY session using
// opencode's wire protocol:
//   - TEXT frames carry raw terminal output bytes (pty -> ws).
//   - BINARY frames carry control/meta: leading 0x00 then UTF-8 JSON.
//   - Input (ws -> pty) is raw bytes regardless of frame type.
//
// Output is sourced from the pty's buffered fan-out (Subscribe), NOT a direct
// ptmx read: the pty package owns the single ptmx reader, so reading ptmx here
// would steal bytes from that reader.
func (s *Server) handlePtyConnect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("ptyID")
	p, ok := s.ptys.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "pty not found")
		return
	}
	ticket := r.URL.Query().Get("ticket")
	if ticket != "" && !p.RedeemTicket(ticket) {
		writeError(w, http.StatusForbidden, "invalid or expired ticket")
		return
	}
	// empty ticket: allowed (loopback no-auth posture, matches opencode)
	if p.Ptmx() == nil {
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

	// Cancelable context tied to this connection. r.Context() alone is NOT
	// cancelled when the websocket closes, so deriving a cancelable context
	// and cancelling it on teardown lets the pump-out goroutine exit.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Subscribe to the pty fan-out: snapshot backlog + live channel.
	backlog, ch, _, unsub := p.Subscribe()
	defer unsub()

	// Server speaks first: one BINARY meta frame, leading 0x00 then JSON.
	meta := append([]byte{0x00}, []byte(`{"cursor":0}`)...)
	if err := c.Write(ctx, websocket.MessageBinary, meta); err != nil {
		return
	}

	// Replay backlog as TEXT frames, chunked to <=64KiB per frame.
	const chunkSize = 65536
	for i := 0; i < len(backlog); i += chunkSize {
		end := i + chunkSize
		if end > len(backlog) {
			end = len(backlog)
		}
		if err := c.Write(ctx, websocket.MessageText, backlog[i:end]); err != nil {
			return
		}
	}

	// pump-out: live chunks -> websocket as raw TEXT frames (goroutine).
	go func() {
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-ch:
				if !ok {
					return
				}
				if err := c.Write(ctx, websocket.MessageText, chunk); err != nil {
					return
				}
			}
		}
	}()

	// pump-in: websocket -> pty (main loop). Any frame type is raw input.
	// On return (ws closed/read error) the outer `defer cancel()` fires,
	// which unblocks the pump-out goroutine.
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if len(data) > 0 {
			if _, werr := p.WriteInput(data); werr != nil {
				return
			}
		}
	}
}
