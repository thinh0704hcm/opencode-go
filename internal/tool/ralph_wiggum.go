package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type ralphInput struct {
	Problem string `json:"problem"`
	Mode    string `json:"mode,omitempty"` // simple|risks|next-step
}

type ralphOutput struct {
	Result string `json:"result"`
}

type ralphWiggumTool struct{}

func (ralphWiggumTool) Name() string   { return "ralph_wiggum" }
func (ralphWiggumTool) Mutating() bool { return false }

func NewRalphWiggumTool() Tool { return ralphWiggumTool{} }

func (ralphWiggumTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in ralphInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.Problem) == "" {
		return Result{}, fmt.Errorf("ralph_wiggum: problem required")
	}
	mode := strings.TrimSpace(in.Mode)
	var res string
	switch mode {
	case "simple":
		res = fmt.Sprintf("Simplified: %s", in.Problem)
	case "risks":
		res = "Potential risks: none identified."
	case "next-step":
		res = "Next step: Review the problem and plan actions."
	default:
		// combine all
		parts := []string{
			fmt.Sprintf("Simplified: %s", in.Problem),
			"Potential risks: none identified.",
			"Next step: Review the problem and plan actions.",
		}
		res = strings.Join(parts, " \n")
	}
	out := ralphOutput{Result: res}
	b, err := json.Marshal(out)
	if err != nil {
		return Result{}, fmt.Errorf("marshal error: %w", err)
	}
	return Result{Output: string(b)}, nil
}
