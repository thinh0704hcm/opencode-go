package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type ohMyOpencodeTool struct{}

type toolInfo struct {
	Name     string `json:"name"`
	Mutating bool   `json:"mutating"`
	Category string `json:"category,omitempty"`
}

type ohMyOpencodeOutput struct {
	Tools    []toolInfo `json:"tools"`
	Routes   []string   `json:"routes,omitempty"`
	Features []string   `json:"features,omitempty"`
}

func (ohMyOpencodeTool) Name() string   { return "oh_my_opencode" }
func (ohMyOpencodeTool) Mutating() bool { return false }

func (ohMyOpencodeTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Topic string `json:"topic,omitempty"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if len(in.Topic) > 1<<20 {
		return Result{}, errors.New("topic input exceeds 1MiB limit")
	}
	// Build dynamic tool list from default registry
	registry := NewDefaultRegistry()
	toolsList := registry.List()
	var outTools []toolInfo
	lower := strings.ToLower(in.Topic)
	for _, t := range toolsList {
		name := t.Name()
		if in.Topic == "" || strings.Contains(strings.ToLower(name), lower) {
			outTools = append(outTools, toolInfo{Name: name, Mutating: t.Mutating()})
		}
	}
	// Output JSON structure
	out := ohMyOpencodeOutput{Tools: outTools}
	b, err := json.Marshal(out)
	if err != nil {
		return Result{}, err
	}
	return Result{Output: string(b)}, nil
}
