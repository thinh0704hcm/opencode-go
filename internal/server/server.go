package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/permission"
	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/pty"
	"github.com/opencode-go/opencode-go/internal/session"
	"github.com/opencode-go/opencode-go/internal/tool"
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
	tools    *tool.Registry
	workdir  string
	ptys     *pty.Registry

	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc

	http *http.Server
}

// Options configures a Server.
type Options struct {
	Provider provider.Provider
	Model    string // default model id
	Logger   *slog.Logger
	Tools    *tool.Registry
	Workdir  string
}

// New builds a Server with its in-memory bus, store, and permission gate.
func New(opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	tools := opts.Tools
	if tools == nil {
		tools = tool.NewDefaultRegistry()
	}
	workdir := opts.Workdir
	if workdir == "" {
		workdir = "."
	}
	return &Server{
		bus:      event.NewBus(),
		store:    session.NewStore(),
		perms:    permission.NewStore(),
		provider: opts.Provider,
		model:    opts.Model,
		logger:   logger,
		tools:    tools,
		workdir:  workdir,
		ptys:     pty.NewRegistry(),
		cancels:  map[string]context.CancelFunc{},
	}
}

// Handler returns the HTTP handler (router) for the server.
func (s *Server) Handler() http.Handler {
	return s.routes()
}

// registerCancel records the cancel func for an in-flight session turn.
func (s *Server) registerCancel(sessionID string, cancel context.CancelFunc) {
	s.cancelMu.Lock()
	s.cancels[sessionID] = cancel
	s.cancelMu.Unlock()
}

// clearCancel removes the cancel func for a session once its turn ends.
func (s *Server) clearCancel(sessionID string) {
	s.cancelMu.Lock()
	delete(s.cancels, sessionID)
	s.cancelMu.Unlock()
}

// cancelSession cancels the in-flight turn for a session, returning true if one
// was registered.
func (s *Server) cancelSession(sessionID string) bool {
	s.cancelMu.Lock()
	c, ok := s.cancels[sessionID]
	s.cancelMu.Unlock()
	if ok && c != nil {
		c()
		return true
	}
	return false
}

// ListenAndServe binds to addr (expected 127.0.0.1:port) and serves until
// shutdown. Bind address is enforced by the caller (architecture §11).
func (s *Server) ListenAndServe(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) && os.Getenv("OPENCODE_GO_ALLOW_NONLOOPBACK") != "1" {
			return fmt.Errorf("refusing non-loopback bind %q (set OPENCODE_GO_ALLOW_NONLOOPBACK=1 to override)", addr)
		}
	}
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
