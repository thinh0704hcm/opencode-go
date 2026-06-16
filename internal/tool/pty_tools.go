package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/opencode-go/opencode-go/internal/pty"
	"github.com/opencode-go/opencode-go/internal/session"
)

var defaultPtys = pty.NewRegistry()

type ptySpawnTool struct{}
type ptyWriteTool struct{}
type ptyReadTool struct{}
type ptyListTool struct{}
type ptyKillTool struct{}

func (ptySpawnTool) Name() string   { return "pty_spawn" }
func (ptySpawnTool) Mutating() bool { return true }
func (ptySpawnTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var req struct {
		Command        string   `json:"command"`
		Args           []string `json:"args"`
		Title          string   `json:"title"`
		Description    string   `json:"description"`
		Workdir        string   `json:"workdir"`
		TimeoutSeconds int      `json:"timeoutSeconds"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(req.Command) == "" {
		return Result{}, errors.New("command required")
	}
	cwd := req.Workdir
	if cwd != "" {
		abs, err := sb.Resolve(cwd)
		if err != nil {
			return Result{}, err
		}
		cwd = abs
	} else {
		cwd = sb.Root()
	}
	id := session.NewID("pty")
	title := req.Title
	if title == "" {
		title = req.Description
	}
	p, err := defaultPtys.Spawn(id, title, req.Command, req.Args, cwd, req.TimeoutSeconds)
	if err != nil {
		return Result{}, err
	}
	return Result{Output: fmt.Sprintf("PTY spawned: %s\nCommand: %s\nStatus: %s\nUse pty_read/pty_write/pty_kill.", p.ID, p.Command, p.Status)}, nil
}

func (ptyWriteTool) Name() string   { return "pty_write" }
func (ptyWriteTool) Mutating() bool { return true }
func (ptyWriteTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var req struct{ ID, Data string }
	if err := json.Unmarshal(input, &req); err != nil {
		return Result{}, err
	}
	p, ok := defaultPtys.Get(req.ID)
	if !ok {
		return Result{}, fmt.Errorf("pty not found: %s", req.ID)
	}
	_, err := p.WriteInput([]byte(req.Data))
	if err != nil {
		return Result{}, err
	}
	return Result{Output: "ok"}, nil
}

func (ptyReadTool) Name() string   { return "pty_read" }
func (ptyReadTool) Mutating() bool { return false }
func (ptyReadTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var req struct {
		ID         string
		Offset     int
		Limit      int
		Pattern    string
		IgnoreCase bool
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return Result{}, err
	}
	p, ok := defaultPtys.Get(req.ID)
	if !ok {
		return Result{}, fmt.Errorf("pty not found: %s", req.ID)
	}
	if req.Limit == 0 {
		req.Limit = 500
	}
	lines, total, more := p.ReadLines(req.Offset, req.Limit)
	if req.Pattern != "" {
		pat := req.Pattern
		if req.IgnoreCase {
			pat = "(?i)" + pat
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			return Result{}, err
		}
		filtered := lines[:0]
		for _, line := range lines {
			if re.MatchString(line) {
				filtered = append(filtered, line)
			}
		}
		lines = filtered
	}
	return Result{Output: fmt.Sprintf("status=%s lines=%d offset=%d hasMore=%v\n%s", p.Info().Status, total, req.Offset, more, strings.Join(lines, "\n"))}, nil
}

func (ptyListTool) Name() string   { return "pty_list" }
func (ptyListTool) Mutating() bool { return false }
func (ptyListTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	infos := defaultPtys.List()
	b, _ := json.MarshalIndent(infos, "", "  ")
	return Result{Output: string(b)}, nil
}

func (ptyKillTool) Name() string   { return "pty_kill" }
func (ptyKillTool) Mutating() bool { return true }
func (ptyKillTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var req struct {
		ID      string
		Cleanup bool
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return Result{}, err
	}
	ok := defaultPtys.Remove(req.ID)
	if !ok {
		return Result{}, fmt.Errorf("pty not found: %s", req.ID)
	}
	return Result{Output: "ok"}, nil
}

var _ = sync.Mutex{}
