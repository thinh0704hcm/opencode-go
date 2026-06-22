package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

type backgroundStatusOutput struct {
	Supported []string `json:"supported,omitempty"`
}

type backgroundStatusTool struct{}

func (backgroundStatusTool) Name() string   { return "background_status" }
func (backgroundStatusTool) Mutating() bool { return false }

func NewBackgroundStatusTool() Tool { return backgroundStatusTool{} }

func (backgroundStatusTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	// For now, indicate registered background-capable tools.
	// Here we just return empty list.
	out := backgroundStatusOutput{Supported: []string{}}
	b, err := json.Marshal(out)
	if err != nil {
		return Result{}, fmt.Errorf("marshal error: %w", err)
	}
	return Result{Output: string(b)}, nil
}
