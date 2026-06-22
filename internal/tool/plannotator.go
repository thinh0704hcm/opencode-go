package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type plannotatorInput struct {
	Plan   string `json:"plan"`
	Status string `json:"status,omitempty"`
}

type plannotatorTool struct{}

func (plannotatorTool) Name() string   { return "plannotator" }
func (plannotatorTool) Mutating() bool { return false }

func (plannotatorTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in plannotatorInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if len(in.Plan) > 1<<20 {
		return Result{}, errors.New("plan input exceeds 1MiB limit")
	}
	if strings.TrimSpace(in.Plan) == "" {
		return Result{}, errors.New("plan input is required")
	}
	lines := strings.Split(strings.TrimSpace(in.Plan), "\n")
	if len(lines) > 100 {
		return Result{}, errors.New("plan exceeds 100 steps limit")
	}
	var b strings.Builder
	b.WriteString("Annotated plan checklist:\n")
	for i, line := range lines {
		step := strings.TrimSpace(line)
		if step == "" {
			continue
		}
		risk := "low"
		if strings.Contains(strings.ToLower(step), "todo") || strings.Contains(strings.ToLower(step), "risk") {
			risk = "high"
		}
		b.WriteString(fmt.Sprintf("%d. %s [risk: %s]\n", i+1, step, risk))
	}
	if in.Status != "" {
		b.WriteString("Status: " + in.Status + "\n")
	}
	outStr, truncated := TruncateOutput([]byte(b.String()))
	return Result{Output: outStr, Truncated: truncated}, nil
}
