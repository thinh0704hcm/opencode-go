package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type typeInjectInput struct {
	Code   string `json:"code"`
	Symbol string `json:"symbol,omitempty"`
}

type typeInjectTool struct{}

func (typeInjectTool) Name() string   { return "type_inject" }
func (typeInjectTool) Mutating() bool { return false }

func (typeInjectTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in typeInjectInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if len(in.Code) > 1<<20 { // 1MiB cap
		return Result{}, errors.New("code input exceeds 1MiB limit")
	}
	if strings.TrimSpace(in.Code) == "" {
		return Result{}, errors.New("code input is required")
	}
	var suggestion string
	prefix := "Heuristic suggestions only; no AST/typechecker mutation. "
	if in.Symbol != "" {
		suggestion = fmt.Sprintf(prefix+"Add/strengthen type for symbol '%s' in provided code. Suggestion: use explicit type annotations.", in.Symbol)
	} else {
		suggestion = prefix + "Suggest adding type annotations to the provided code using explicit types where possible."
	}
	out, truncated := TruncateOutput([]byte(suggestion))
	return Result{Output: out, Truncated: truncated}, nil
}
