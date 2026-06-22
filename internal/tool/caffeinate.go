package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
)

type caffeinateInput struct {
	Seconds int `json:"seconds"`
}

type caffeinateTool struct{}

func (caffeinateTool) Name() string   { return "caffeinate" }
func (caffeinateTool) Mutating() bool { return false }

func NewCaffeinateTool() Tool { return caffeinateTool{} }

func (caffeinateTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in caffeinateInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if in.Seconds <= 0 {
		return Result{}, fmt.Errorf("seconds must be > 0")
	}
	// Cap to 3600 seconds.
	if in.Seconds > 3600 {
		in.Seconds = 3600
	}
	// Try common commands; ignore failures.
	// macOS caffeinate, Linux systemd-inhibit (sleep).
	cmds := [][]string{{"caffeinate", "-t", strconv.Itoa(in.Seconds)}, {"systemd-inhibit", "--what=idle", "sleep", strconv.Itoa(in.Seconds)}}
	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		if err := cmd.Run(); err == nil {
			return Result{Output: fmt.Sprintf("%s succeeded", args[0])}, nil
		}
	}
	// No command succeeded; no‑op.
	return Result{Output: "no-op (unsupported env)"}, nil
}
