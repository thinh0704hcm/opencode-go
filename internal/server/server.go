package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/permission"
	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/session"
)

// Version reported by /global/health for handshake parity (architecture §2.1).
const Version = "1.16.0"

// Server holds the runtime dependencies and HTTP lifecycle (architecture §2.1).
type Server struct {
	bus      *event.Bus
	store    *session.Store
	perms    *permission.Store
	provider provider.Provider
	model    string // default model id passed to the provider
	logger   *slog.Logger

	http *http.Server
}

// Options configures a Server.
type Options struct {
	Provider provider.Provider
	Model    string // default model id
	Logger   *slog.Logger
}

// New builds a Server with its in-memory bus, store, and permission gate.
func New(opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		bus:      event.NewBus(),
		store:    session.NewStore(),
		perms:    permission.NewStore(),
		provider: opts.Provider,
		model:    opts.Model,
		logger:   logger,
	}
}

// Handler returns the HTTP handler (router) for the server.
func (s *Server) Handler() http.Handler {
	return s.routes()
}

// ListenAndServe binds to addr (expected 127.0.0.1:port) and serves until
// shutdown. Bind address is enforced by the caller (architecture §11).
func (s *Server) ListenAndServe(addr string) error {
	s.http = &http.Server{
		Addr:    addr,
		Handler: s.routes(),
	}
	return s.http.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

// heartbeatInterval keeps idle SSE streams alive (architecture §2.3/§7.3).
const heartbeatInterval = 15 * time.Second
