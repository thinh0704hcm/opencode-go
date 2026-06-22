package server

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/opencode-go/opencode-go/internal/config"
	"github.com/opencode-go/opencode-go/internal/provider"
)

// directoryParam returns the optional ?directory=<cwd> query value, falling back
// to the server process working directory when the param is empty.
func directoryParam(r *http.Request) string {
	if dir := r.URL.Query().Get("directory"); dir != "" {
		return dir
	}
	if dir := r.URL.Query().Get("path"); dir != "" {
		return dir
	}
	if dir := os.Getenv("OPENCODE_GO_WORKDIR"); dir != "" {
		return dir
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return ""
}

// pathResponse is the GET /path body: the resolved home/state/config locations
// plus the active worktree/directory.
type pathResponse struct {
	Home      string `json:"home"`
	State     string `json:"state"`
	Config    string `json:"config"`
	Worktree  string `json:"worktree"`
	Directory string `json:"directory"`
}

// handlePath serves GET /path. home/state/config are derived from the OS user
// home dir; worktree and directory both reflect the ?directory= value (or the
// server cwd fallback).
func (s *Server) handlePath(w http.ResponseWriter, r *http.Request) {
	dir := directoryParam(r)

	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	writeJSON(w, http.StatusOK, pathResponse{
		Home:      home,
		State:     filepath.Join(home, ".local", "state", "opencode"),
		Config:    filepath.Join(home, ".config", "opencode"),
		Worktree:  dir,
		Directory: dir,
	})
}

// projectTime carries the created/updated millisecond timestamps.
type projectTime struct {
	Created int64 `json:"created"`
	Updated int64 `json:"updated"`
}

// projectResponse is the GET /project/current body.
type projectResponse struct {
	ID        string      `json:"id"`
	Worktree  string      `json:"worktree"`
	Time      projectTime `json:"time"`
	Sandboxes []any       `json:"sandboxes"`
}

func (s *Server) handleProjectList(w http.ResponseWriter, r *http.Request) {
	worktree := directoryParam(r)
	id := filepath.Base(worktree)
	if worktree == "" || id == "." || id == string(filepath.Separator) {
		id = "global"
	}
	nowMS := time.Now().UnixMilli()
	writeJSON(w, http.StatusOK, []projectResponse{{
		ID:        id,
		Worktree:  worktree,
		Time:      projectTime{Created: nowMS, Updated: nowMS},
		Sandboxes: []any{},
	}})
}

// handleProjectCurrent serves GET /project/current. worktree = ?directory=; id
// is the base name of the worktree (or "global" when empty); both timestamps
// use the current time in milliseconds.
func (s *Server) handleProjectCurrent(w http.ResponseWriter, r *http.Request) {
	worktree := directoryParam(r)

	id := filepath.Base(worktree)
	if worktree == "" || id == "." || id == string(filepath.Separator) {
		id = "global"
	}

	nowMS := time.Now().UnixMilli()

	writeJSON(w, http.StatusOK, projectResponse{
		ID:        id,
		Worktree:  worktree,
		Time:      projectTime{Created: nowMS, Updated: nowMS},
		Sandboxes: []any{},
	})
}

// authMethod is one accepted auth method for a provider in GET /provider/auth.
type authMethod struct {
	Type  string `json:"type"`
	Label string `json:"label"`
}

// handleProviderAuth serves GET /provider/auth: a map keyed by each provider id
// from the registry, with the API-key auth method as the value.
func (s *Server) handleProviderAuth(w http.ResponseWriter, r *http.Request) {
	reg := provider.BuildRegistry(config.Load(directoryParam(r)))

	out := map[string][]authMethod{}
	for _, p := range reg.Providers {
		out[p.ID] = []authMethod{{Type: "api", Label: "API Key"}}
	}

	writeJSON(w, http.StatusOK, out)
}

// consoleResponse is the GET /experimental/console body.
type consoleResponse struct {
	ConsoleManagedProviders []any `json:"consoleManagedProviders"`
	SwitchableOrgCount      int   `json:"switchableOrgCount"`
}

// handleExperimentalConsole serves GET /experimental/console.
func (s *Server) handleExperimentalConsole(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, consoleResponse{
		ConsoleManagedProviders: []any{},
		SwitchableOrgCount:      0,
	})
}

// vcsResponse is the GET /vcs body.
type vcsResponse struct {
	Branch        *string `json:"branch"`
	DefaultBranch *string `json:"default_branch"`
}

type commandInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Agent       any      `json:"agent"`
	Model       any      `json:"model"`
	Source      string   `json:"source"`
	Template    string   `json:"template"`
	Hints       []string `json:"hints"`
}

// handleCommand serves GET /command, loading global + project markdown commands
// and inline opencode.json command entries. Ctrl+P depends on this list being
// stable across TUI restarts.
func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	workdir := directoryOf(r)
	if workdir == "" {
		workdir = s.workdir
	}
	writeJSON(w, http.StatusOK, loadCommands(workdir))
}

func loadCommands(workdir string) []commandInfo {
	byName := map[string]commandInfo{}
	for _, dir := range commandDirs(workdir) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			cmd, ok := parseCommandFile(filepath.Join(dir, e.Name()))
			if !ok {
				continue
			}
			if cmd.Name == "" {
				cmd.Name = strings.TrimSuffix(e.Name(), ".md")
			}
			cmd.Source = "command"
			if cmd.Hints == nil {
				cmd.Hints = []string{"$ARGUMENTS"}
			}
			byName[cmd.Name] = cmd
		}
	}

	cfg := config.Load(workdir)
	if m, ok := cfg.Raw["command"].(map[string]any); ok {
		for name, raw := range m {
			obj, _ := raw.(map[string]any)
			cmd := commandInfo{Name: name, Source: "config", Agent: nil, Model: nil, Hints: []string{"$ARGUMENTS"}}
			if v, ok := obj["description"].(string); ok {
				cmd.Description = v
			}
			if v, ok := obj["template"].(string); ok {
				cmd.Template = v
			}
			if v, ok := obj["prompt"].(string); ok && cmd.Template == "" {
				cmd.Template = v
			}
			if v, ok := obj["agent"]; ok {
				cmd.Agent = v
			}
			if v, ok := obj["model"]; ok {
				cmd.Model = v
			}
			byName[name] = cmd
		}
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]commandInfo, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out
}

func commandDirs(workdir string) []string {
	dirs := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", "opencode", "command"), filepath.Join(home, ".config", "opencode", "commands"))
	}
	if workdir != "" {
		dirs = append(dirs, filepath.Join(workdir, ".opencode", "command"), filepath.Join(workdir, ".opencode", "commands"))
	}
	return dirs
}

func parseCommandFile(path string) (commandInfo, bool) {
	f, err := os.Open(path)
	if err != nil {
		return commandInfo{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var cmd commandInfo
	cmd.Agent = nil
	cmd.Model = nil
	inFront := false
	frontDone := false
	lineNo := 0
	var body strings.Builder
	for sc.Scan() {
		line := sc.Text()
		lineNo++
		trimmed := strings.TrimSpace(line)
		if lineNo == 1 && trimmed == "---" {
			inFront = true
			continue
		}
		if inFront && !frontDone {
			if trimmed == "---" {
				frontDone = true
				continue
			}
			parseCommandFrontLine(&cmd, line)
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	cmd.Template = strings.TrimSpace(body.String())
	return cmd, cmd.Template != "" || cmd.Description != ""
}

func parseCommandFrontLine(cmd *commandInfo, line string) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return
	}
	key := strings.ToLower(strings.TrimSpace(line[:idx]))
	val := strings.Trim(strings.TrimSpace(line[idx+1:]), `"'`)
	switch key {
	case "name":
		cmd.Name = val
	case "description":
		cmd.Description = val
	case "agent":
		if val != "" {
			cmd.Agent = val
		}
	case "model":
		if val != "" {
			cmd.Model = val
		}
	}
}

// handleFormatter serves GET /formatter -> [].
func (s *Server) handleFormatter(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

// handleLSP serves GET /lsp -> [].
func (s *Server) handleLSP(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

// handleSessionStatus serves GET /session/status -> {}.
func (s *Server) handleSessionStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{})
}

// handleExperimentalResource serves GET /experimental/resource -> {}.
func (s *Server) handleExperimentalResource(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{})
}

// handleExperimentalWorkspace serves GET /experimental/workspace -> [].
func (s *Server) handleExperimentalWorkspace(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

// handleExperimentalWorkspaceStatus serves GET /experimental/workspace/status.
// The TS TUI reads `response.data` and calls `.map` on it, so `data` MUST be an
// array (an empty object/`{}` makes the TUI throw "(...).map is not a function").
func (s *Server) handleExperimentalWorkspaceStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}

func (s *Server) handleProviderOAuthNoop(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{})
}

// SDK Drop-in stubs

func (s *Server) handleGlobalConfigGet(w http.ResponseWriter, r *http.Request) {
	cfg := config.Load(s.workdir)
	out := cfg.Defaulted()
	maskSecretsDeep(out)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGlobalConfigUpdate(w http.ResponseWriter, r *http.Request) {
    if !requireJSON(w, r) {
        return
    }
    var body map[string]any
    if !decodeStrictBody(w, r, &body, false) {
        return
    }
    // Reload and return the current masked config (TS parity: PATCH returns config info).
    cfg := config.Load(s.workdir)
    out := cfg.Defaulted()
    maskSecretsDeep(out)
    writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleExperimentalConsoleOrgs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

func (s *Server) handleExperimentalSessionList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "cursor": nil})
}

func (s *Server) handleExperimentalWorkspaceAdapter(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

func (s *Server) handleExperimentalWorktreeList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

func (s *Server) handleProjectDirectories(w http.ResponseWriter, r *http.Request) {
	// 404 stub for GET /project/{id}/directories
	// Return current directory as main
	workdir := s.workdir
	if dirQuery := r.URL.Query().Get("directory"); dirQuery != "" && filepath.IsAbs(dirQuery) {
		workdir = dirQuery
	}
	writeJSON(w, http.StatusOK, []map[string]any{
		{"directory": workdir, "type": "main"},
	})
}

func (s *Server) handleAPIReference(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}

func (s *Server) handleAPIIntegration(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}
