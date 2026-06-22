package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

type notifierInput struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

type notifierTool struct{}

func (notifierTool) Name() string   { return "notifier" }
func (notifierTool) Mutating() bool { return false }

func NewNotifierTool() Tool { return notifierTool{} }

func (notifierTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in notifierInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	// Try notify-send; ignore errors in headless env.
	cmd := exec.CommandContext(ctx, "notify-send", "--", in.Title, in.Message)
	if err := cmd.Run(); err != nil {
		// Return success with note.
		out := fmt.Sprintf("notify-send failed (likely headless): %v", err)
		return Result{Output: out}, nil
	}
	return Result{Output: "sent"}, nil
}
