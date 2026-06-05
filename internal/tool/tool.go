package tool

import (
	"context"
	"encoding/json"
	"sort"
)

// Result is a tool execution result.
type Result struct {
	Output    string `json:"output"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Tool is a callable capability. Execute must NOT run anything if the sandbox
// rejects a path; return a non-nil error instead.
type Tool interface {
	Name() string
	Mutating() bool
	Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error)
}

// Registry holds tools by name.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool keyed by its Name().
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Lookup returns the tool registered under name.
func (r *Registry) Lookup(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List returns the registered tools in stable order sorted by name.
func (r *Registry) List() []Tool {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Tool, 0, len(names))
	for _, name := range names {
		out = append(out, r.tools[name])
	}
	return out
}
