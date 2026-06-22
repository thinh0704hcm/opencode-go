package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type micodeInput struct {
	Request  string `json:"request"`
	MaxSteps int    `json:"max_steps,omitempty"`
}

type micodeOutput struct {
	Steps  []string `json:"steps"`
	Verify string   `json:"verify"`
}

type micodeTool struct{}

func (micodeTool) Name() string   { return "micode" }
func (micodeTool) Mutating() bool { return false }

func NewMicodeTool() Tool { return micodeTool{} }

func (micodeTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in micodeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.Request) == "" {
		return Result{}, fmt.Errorf("micode: request required")
	}
	// Limit request size to 1MiB
	if len(in.Request) > 1<<20 {
		return Result{}, fmt.Errorf("micode: request too large")
	}
	max := in.MaxSteps
	if max <= 0 {
		max = 3
	}
	if max > 20 {
		max = 20
	}
	// deterministic steps based on request words
	words := strings.Fields(in.Request)
	steps := []string{}
	for i := 0; i < max && i < 3; i++ {
		step := fmt.Sprintf("Step %d: %s", i+1, strings.Title(words[i%len(words)]))
		steps = append(steps, step)
	}
	out := micodeOutput{Steps: steps, Verify: "All steps deterministic"}
	b, err := json.Marshal(out)
	if err != nil {
		return Result{}, fmt.Errorf("marshal error: %w", err)
	}
	return Result{Output: string(b)}, nil
}
