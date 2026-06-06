package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/coder/websocket"
)

// handlePtyConnect bridges a websocket to an existing PTY session using
// opencode's wire protocol:
//   - TEXT frames carry raw terminal output bytes (ptmx -> ws).
//   - BINARY frames carry control/meta: leading 0x00 then UTF-8 JSON.
//   - Input (ws -> ptmx) is raw bytes regardless of frame type.
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
	// Optional ?cursor= query. We have no ring buffer yet, so every value
	// (-1 live tail, >=0 backlog, absent/invalid) starts live; parse only
	// to avoid surprising behavior and to stay protocol-compatible.
	_, _ = strconv.Atoi(r.URL.Query().Get("cursor"))

	// InsecureSkipVerify is acceptable: the server binds 127.0.0.1 only
	// (loopback, no-auth posture), so Origin/CORS checks are not needed.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusInternalError, "closing")

	// Cancelable context tied to this connection. r.Context() alone is NOT
	// cancelled when the websocket closes, so deriving a cancelable context
	// and cancelling it on teardown is what lets pump 1 exit (instead of
	// leaking a goroutine that keeps reading the shared ptmx forever).
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Server speaks first: one BINARY meta frame, leading 0x00 then JSON.
	// We have no backlog buffer in M5, so report cursor=0.
	meta := append([]byte{0x00}, []byte(`{"cursor":0}`)...)
	if err := c.Write(ctx, websocket.MessageBinary, meta); err != nil {
		return
	}

	// pump 1: ptmx -> websocket as raw TEXT frames (goroutine).
	//
	// A goroutine blocked on ptmx.Read does not observe ctx cancellation on
	// its own, and we must NOT close the shared ptmx (other reconnects may
	// attach). To bound teardown latency we set a short read deadline so the
	// read returns periodically; on a timeout we re-check ctx and exit if the
	// connection is gone. This guarantees the goroutine is torn down within
	// ~1s of cancel(), avoiding the goroutine/byte-stealing leak on reconnect.
	go func() {
		defer cancel()
		buf := make([]byte, 4096)
		deadlineSupported := true
		for {
			if ctx.Err() != nil {
				return
			}
			if deadlineSupported {
				if derr := ptmx.SetReadDeadline(time.Now().Add(1 * time.Second)); derr != nil {
					// ptmx does not support deadlines; fall back to a plain
					// blocking read for the remainder of the connection.
					deadlineSupported = false
				}
			}
			n, err := ptmx.Read(buf)
			if n > 0 {
				if werr := c.Write(ctx, websocket.MessageText, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				if deadlineSupported && (errors.Is(err, os.ErrDeadlineExceeded) || os.IsTimeout(err)) {
					if ctx.Err() != nil {
						return
					}
					continue
				}
				return
			}
		}
	}()

	// pump 2: websocket -> ptmx (main loop). Any frame type is raw input.
	// On return (ws closed/read error) the outer `defer cancel()` fires,
	// which unblocks pump 1.
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
