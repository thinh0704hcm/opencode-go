package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type conductorInput struct {
	Tasks interface{} `json:"tasks"` // can be []string or raw text
}

type conductorTool struct{}

func (conductorTool) Name() string   { return "conductor" }
func (conductorTool) Mutating() bool { return false }

func (conductorTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in conductorInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	// Normalize tasks to slice of strings
	var tasks []string
	switch v := in.Tasks.(type) {
	case []interface{}:
		for _, x := range v {
			if s, ok := x.(string); ok {
				tasks = append(tasks, s)
			} else {
				return Result{}, errors.New("tasks array must contain strings")
			}
		}
	case string:
		// split on newlines or commas
		parts := strings.FieldsFunc(v, func(r rune) bool { return r == '\n' || r == ',' })
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				tasks = append(tasks, p)
			}
		}
	default:
		return Result{}, errors.New("tasks must be string or array of strings")
	}
	if len(tasks) > 100 {
		return Result{}, errors.New("tasks exceed 100 limit")
	}
	// Validate individual task size (8 KiB)
	for _, t := range tasks {
		if len(t) > 8*1024 {
			return Result{}, errors.New("individual task exceeds 8KiB limit")
		}
	}
	// Preserve input order; classify each task
	var b strings.Builder
	b.WriteString("Execution order:\n")
	for i, t := range tasks {
		category := "independent"
		lowered := strings.ToLower(t)
		if strings.Contains(lowered, "review") || strings.Contains(lowered, "test") {
			category = "dependent"
		}
		b.WriteString(fmt.Sprintf("%d. %s [%s]\n", i+1, t, category))
	}
	outStr, truncated := TruncateOutput([]byte(b.String()))
	return Result{Output: outStr, Truncated: truncated}, nil
}
