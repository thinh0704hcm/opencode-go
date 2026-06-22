package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type speckitChainInput struct {
	Spec  string `json:"spec"`
	Phase string `json:"phase,omitempty"`
}

type speckitChainTool struct{}

func (speckitChainTool) Name() string   { return "speckit_chain" }
func (speckitChainTool) Mutating() bool { return false }

func (speckitChainTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in speckitChainInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if len(in.Spec) > 1<<20 {
		return Result{}, errors.New("spec input exceeds 1MiB limit")
	}
	if strings.TrimSpace(in.Spec) == "" {
		return Result{}, errors.New("spec input is required")
	}
	phases := []string{"clarify", "design", "tasks", "tests", "release"}
	if in.Phase != "" {
		// prepend provided phase
		phases = append([]string{in.Phase}, phases...)
	}
	out := fmt.Sprintf("Ordered spec chain: %s", strings.Join(phases, " -> "))
	outStr, truncated := TruncateOutput([]byte(out))
	return Result{Output: outStr, Truncated: truncated}, nil
}
