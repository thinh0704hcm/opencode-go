package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type svelteAssistInput struct {
	Component string `json:"component"`
	Goal      string `json:"goal,omitempty"`
}

type svelteAssistTool struct{}

func (svelteAssistTool) Name() string   { return "svelte_assist" }
func (svelteAssistTool) Mutating() bool { return false }

func (svelteAssistTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in svelteAssistInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if len(in.Component) > 1<<20 {
		return Result{}, errors.New("component input exceeds 1MiB limit")
	}
	if strings.TrimSpace(in.Component) == "" {
		return Result{}, errors.New("component input is required")
	}
	checklist := []string{
		"Use $: reactive statements for derived values",
		"Prefer stores for shared state (writable, readable, derived)",
		"Avoid direct DOM manipulation; use bind: directives",
		"Ensure accessibility: add aria-labels, proper focus management",
		"Handle events with on:click etc., prevent default when needed",
		"Watch for common pitfalls: memory leaks from unsubscribed stores",
	}
	if in.Goal != "" {
		checklist = append([]string{fmt.Sprintf("Goal: %s", in.Goal)}, checklist...)
	}
	out := "Svelte assistance checklist:\n" + fmt.Sprintf("%s", checklist[0])
	for _, item := range checklist[1:] {
		out += "\n- " + item
	}
	outStr, truncated := TruncateOutput([]byte(out))
	return Result{Output: outStr, Truncated: truncated}, nil
}
