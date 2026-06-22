package tool

import (
	"context"
	"encoding/json"
	"unicode/utf8"
)

type flashCompactInput struct {
	Output string `json:"output"`
	Budget int    `json:"budget"`
}

type flashCompactTool struct{}

func (flashCompactTool) Name() string   { return "flashcompact" }
func (flashCompactTool) Mutating() bool { return false }

func NewFlashCompactTool() Tool { return flashCompactTool{} }

func (flashCompactTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in flashCompactInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if in.Budget <= 0 {
		in.Budget = 1024
	}
	// Truncate UTF‑8 safely.
	bytes := []byte(in.Output)
	if len(bytes) <= in.Budget {
		return Result{Output: in.Output, Truncated: false}, nil
	}
	// Walk runes.
	var pos int
	var count int
	for i := 0; i < len(bytes) && count < in.Budget; {
		r, size := utf8.DecodeRune(bytes[i:])
		if r == utf8.RuneError && size == 1 {
			// invalid byte, treat as single byte.
			size = 1
		}
		if count+size > in.Budget {
			break
		}
		i += size
		pos = i
		count += size
	}
	truncated := true
	out := string(bytes[:pos]) + "...(truncated)"
	return Result{Output: out, Truncated: truncated}, nil
}
