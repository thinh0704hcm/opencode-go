package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type shellStrategyOutput struct {
	Strategy   string `json:"strategy,omitempty"`
	DryRun     bool   `json:"dry_run,omitempty"`
	Configured bool   `json:"configured,omitempty"`
}

type shellStrategyTool struct{}

func (shellStrategyTool) Name() string   { return "shell_strategy" }
func (shellStrategyTool) Mutating() bool { return false }

func NewShellStrategyTool() Tool { return shellStrategyTool{} }

func (shellStrategyTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	// No input needed.
	strategy := os.Getenv("DCP_STRATEGY")
	dryRunEnv := os.Getenv("DCP_DRY_RUN")
	dryRun := dryRunEnv == "1" || strings.EqualFold(dryRunEnv, "true")
	cfg := strategy != ""
	out := shellStrategyOutput{Strategy: strategy, DryRun: dryRun, Configured: cfg}
	b, err := json.Marshal(out)
	if err != nil {
		return Result{}, fmt.Errorf("marshal error: %w", err)
	}
	return Result{Output: string(b)}, nil
}
