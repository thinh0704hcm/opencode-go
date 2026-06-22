package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type myStatusOutput struct {
	Sessions  int             `json:"sessions"`
	Provider  string          `json:"provider,omitempty"`
	Model     string          `json:"model,omitempty"`
	Workdir   string          `json:"workdir,omitempty"`
	Version   string          `json:"version,omitempty"`
	ToolCount int             `json:"tool_count,omitempty"`
	Features  map[string]bool `json:"features,omitempty"`
	GitDirty  string          `json:"git_dirty,omitempty"`
}

type myStatusTool struct{}

func (myStatusTool) Name() string   { return "mystatus" }
func (myStatusTool) Mutating() bool { return false }

func NewMyStatusTool() Tool { return myStatusTool{} }

func (myStatusTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	// Placeholder sessions.
	sess := 0
	provider := os.Getenv("OPENCODE_PROVIDER")
	model := os.Getenv("OPENCODE_MODEL")
	version := os.Getenv("OPENCODE_VERSION")
	if version == "" {
		version = "dev"
	}

	// Determine workdir.
	cwd := ""
	if sb != nil {
		cwd = sb.Root()
	} else {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}

	// Tool count.
	toolCount := len(NewDefaultRegistry().List())

	// Feature flags.
	features := map[string]bool{
		"vibeguard":       os.Getenv("VIBEGUARD") != "",
		"devcontainer":    os.Getenv("DEVCONTAINER") != "",
		"browser_open":    os.Getenv("BROWSER_OPEN") != "",
		"remote_adapters": os.Getenv("REMOTE_ADAPTERS") != "",
	}

	// Git dirty if .git present.
	gitDirty := ""
	if cwd != "" {
		if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
			gitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(gitCtx, "git", "status", "--porcelain")
			cmd.Dir = cwd
			out, err := cmd.Output()
			if err == nil {
				gitDirty = strings.TrimSpace(string(out))
			}
		}
	}

	out := myStatusOutput{Sessions: sess, Provider: provider, Model: model, Workdir: cwd, Version: version, ToolCount: toolCount, Features: features, GitDirty: gitDirty}
	b, err := json.Marshal(out)
	if err != nil {
		return Result{}, fmt.Errorf("failed to marshal status: %w", err)
	}
	return Result{Output: string(b)}, nil
}
