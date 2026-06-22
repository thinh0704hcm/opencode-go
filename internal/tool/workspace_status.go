package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

type workspaceStatusOutput struct {
	Workdir      string `json:"workdir,omitempty"`
	Devcontainer bool   `json:"devcontainer,omitempty"`
	Daytona      bool   `json:"daytona,omitempty"`
}

type workspaceStatusTool struct{}

func (workspaceStatusTool) Name() string   { return "workspace_status" }
func (workspaceStatusTool) Mutating() bool { return false }

func NewWorkspaceStatusTool() Tool { return workspaceStatusTool{} }

func (workspaceStatusTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	wd := ""
	if sb != nil {
		wd = sb.Root()
	}
	dev := os.Getenv("DEVCONTAINER") != ""
	day := os.Getenv("DAYTONA_API_KEY") != ""
	out := workspaceStatusOutput{Workdir: wd, Devcontainer: dev, Daytona: day}
	b, err := json.Marshal(out)
	if err != nil {
		return Result{}, fmt.Errorf("marshal error: %w", err)
	}
	return Result{Output: string(b)}, nil
}
