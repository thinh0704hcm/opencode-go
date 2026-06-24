package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/opencode-go/opencode-go/internal/config"
	"github.com/opencode-go/opencode-go/internal/event"
	"github.com/opencode-go/opencode-go/internal/mcp"
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
	bus                  *event.Bus
	store                *session.Store
	perms                *permission.Store
	provider             provider.Provider
	configuredProviderID string // user-visible provider alias (e.g., "concactao")
	model                string // default model id passed to the provider
	maxTokens            int    // output-token budget sent as max_tokens (0 = omit)
	logger               *slog.Logger
	tools                *tool.Registry
	mcp                  *mcp.Manager
	workdir              string
	ptys                 *pty.Registry

	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc

	sesMu    sync.Mutex
	sesQueue map[string]*sessionWork

	http *http.Server
}

// Options configures a Server.
type Options struct {
	Provider             provider.Provider
	ConfiguredProviderID string // user-visible provider alias from config
	Model                string // default model id
	MaxTokens            int    // output-token budget (max_tokens); <1 means omit
	Logger               *slog.Logger
	Tools                *tool.Registry
	Workdir              string
	DataDir              string
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
	mcpMgr := mcp.NewManager(loadMCPSection(workdir))
	for _, adapter := range mcpMgr.Adapters() {
		tools.Register(adapter)
	}
	st := session.NewStore()
	if opts.DataDir != "" {
		if err := st.SetPersistDir(opts.DataDir); err != nil {
			logger.Error("session persistence disabled", "err", err)
		} else if err := st.Load(); err != nil {
			logger.Error("session load failed", "err", err)
		}
	}
	srv := &Server{
		bus:   event.NewBus(),
		store: st,
		perms: permission.NewStoreWithPath(func() string {
			if opts.DataDir != "" {
				return filepath.Join(opts.DataDir, "permissions.json")
			}
			return ""
		}()),
		provider:             opts.Provider,
		configuredProviderID: opts.ConfiguredProviderID,
		model:                opts.Model,
		maxTokens:            opts.MaxTokens,
		logger:               logger,
		tools:                tools,
		mcp:                  mcpMgr,
		workdir:              workdir,
		ptys:                 pty.NewRegistry(),
		cancels:              map[string]context.CancelFunc{},
		sesQueue:             map[string]*sessionWork{},
	}
	srv.tools.Register(delegateTool{srv: srv})
	srv.tools.Register(taskTool{srv: srv})
	logger.Debug("tool registered", "name", "delegate")
	logger.Debug("tool registered", "name", "task")
	srv.tools.Register(todoWriteTool{srv: srv})
	srv.tools.Register(todoReadTool{srv: srv})
	// Web tools backed by the 9Router gateway (search + URL→markdown fetch),
	// reusing the chat provider's gateway/key (or NINEROUTER_URL/NINEROUTER_KEY).
	if base, key := resolveNineRouter(opts.Provider); base != "" {
		hc := &http.Client{Timeout: 60 * time.Second}
		srv.tools.Unregister("webfetch") // replace the naive http.GET fetch
		srv.tools.Register(tool.NewWebFetch9RouterTool(base, key, hc))
		srv.tools.Register(tool.NewWebSearchTool(base, key, hc))
		logger.Debug("tool registered", "name", "webfetch (9router)")
		logger.Debug("tool registered", "name", "websearch")
	}
	// Wire MCP tool-list change notifications.
	mcpMgr.SetToolsChangedCallback(func(server string) {
		// Unregister old MCP tools for this server, then register current adapters.
		prefix := server + "_"
		for _, t := range srv.tools.List() {
			if strings.HasPrefix(t.Name(), prefix) {
				srv.tools.Unregister(t.Name())
			}
		}
		for _, adapter := range mcpMgr.AdaptersFor(server) {
			srv.tools.Register(adapter)
		}
		srv.bus.Publish(event.NewToolsChanged(server))
	})
	return srv
}

// resolveNineRouter returns the 9Router v1 base URL (ending in /v1) and API key
// for the web tools. Prefers NINEROUTER_URL/NINEROUTER_KEY env, otherwise derives
// them from the chat provider's gateway. Returns an empty base when unavailable.
func resolveNineRouter(p provider.Provider) (string, string) {
	base := strings.TrimSpace(os.Getenv("NINEROUTER_URL"))
	key := strings.TrimSpace(os.Getenv("NINEROUTER_KEY"))
	if base != "" {
		base = strings.TrimRight(base, "/")
		if !strings.HasSuffix(base, "/v1") {
			base += "/v1"
		}
		return base, key
	}
	type gateway interface {
		BaseURL() string
		APIKey() string
	}
	if op, ok := p.(gateway); ok {
		if b := strings.TrimRight(op.BaseURL(), "/"); b != "" {
			if key == "" {
				key = op.APIKey()
			}
			return b, key
		}
	}
	return "", ""
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
	// Session busy check helper
	// Returns true if a generation is currently running for the session.
	// Used by handlers to guard against concurrent operations.
	// (wrapper defined below)
	//
	// Note: sesMu is a sync.Mutex, not RWMutex, so we lock for safe access.
	//
	// sessionBusy is defined after this method.
	//
	// -----
	// sessionBusy implementation follows after cancelSession.
	// -----
	// (no functional change to cancelSession itself)
	//
	// Added comment only, method unchanged.
	//
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
// sessionBusy returns true if the session has an active generation running.
func (s *Server) sessionBusy(id string) bool {
	s.sesMu.Lock()
	defer s.sesMu.Unlock()
	w, ok := s.sesQueue[id]
	return ok && w.running
}

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

// Shutdown gracefully stops the HTTP server and flushes all sessions to disk.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.mcp != nil {
		s.mcp.Shutdown()
	}
	s.store.PersistAll()
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

// heartbeatInterval keeps idle SSE streams alive (architecture §2.3/§7.3).
const heartbeatInterval = 15 * time.Second

// loadMCPSection loads the "mcp" config section (server name -> config) from the
// workdir, returning nil when absent.
func loadMCPSection(workdir string) map[string]any {
	cfg := config.Load(workdir)
	if cfg.Raw == nil {
		return nil
	}
	section, _ := cfg.Raw["mcp"].(map[string]any)

	// MCP auto-connect is opt-in: spawning configured MCP servers at boot is
	// off by default so a restart never unexpectedly launches heavy subprocesses
	// (browsers, etc.) on constrained hosts. Set OPENCODE_GO_MCP=1 to enable,
	// or set "enabled": true on individual server entries to bypass the env gate.
	if v := os.Getenv("OPENCODE_GO_MCP"); v != "1" && v != "true" {
		// Check if any server has explicit enabled:true in config.
		hasExplicit := false
		for _, v := range section {
			if srv, ok := v.(map[string]any); ok {
				if e, ok := srv["enabled"].(bool); ok && e {
					hasExplicit = true
					break
				}
			}
		}
		if !hasExplicit {
			return nil
		}
	}
	return section
}
