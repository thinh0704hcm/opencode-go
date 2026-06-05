package event

import (
	"encoding/json"
	"fmt"
	"io"
)

// EndpointKind selects how an event is wrapped for an SSE endpoint.
type EndpointKind int

const (
	// KindEvent is GET /event -> bare Event.
	KindEvent EndpointKind = iota
	// KindGlobalEvent is GET /global/event -> {directory, payload}.
	KindGlobalEvent
)

// GlobalEvent is the /global/event envelope (architecture §7.2).
type GlobalEvent struct {
	Directory string `json:"directory"`
	Payload   Event  `json:"payload"`
}

// Wrap renders an event into its JSON wire form for the given endpoint kind.
// dir is the request's ?directory=<cwd> value echoed into the global envelope.
func Wrap(ev Event, kind EndpointKind, dir string) ([]byte, error) {
	switch kind {
	case KindGlobalEvent:
		return json.Marshal(GlobalEvent{Directory: dir, Payload: ev})
	default:
		return json.Marshal(ev)
	}
}

// WriteFrame writes one SSE frame (`data: <json>\n\n`) and flushes
// (architecture §7.3).
func WriteFrame(w io.Writer, flusher interface{ Flush() }, payload []byte) error {
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}

// WriteHeartbeat writes an SSE comment heartbeat (`:\n\n`) and flushes.
func WriteHeartbeat(w io.Writer, flusher interface{ Flush() }) error {
	if _, err := io.WriteString(w, ":\n\n"); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}
