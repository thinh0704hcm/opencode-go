package server

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
)

// statusWriter wraps http.ResponseWriter to capture the response status code.
// It defaults to 200 since WriteHeader is not always called explicitly.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Flush forwards to the underlying writer when it supports http.Flusher,
// preserving SSE streaming semantics.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards to the underlying writer when it supports http.Hijacker,
// preserving websocket upgrade (pty connect) support.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// loggingMiddleware wraps next and logs method/path/query/status for every
// request. Bodies are never logged to avoid leaking secrets.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		logger.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"status", sw.status,
		)
	})
}
