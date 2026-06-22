package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type octtoInput struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels,omitempty"`
}

type octtoTool struct{}

func (octtoTool) Name() string   { return "octto" }
func (octtoTool) Mutating() bool { return false }

func NewOcttoTool() Tool { return octtoTool{} }

func (octtoTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in octtoInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.Title) == "" {
		return Result{}, fmt.Errorf("octto: title required")
	}
	if strings.TrimSpace(in.Body) == "" {
		return Result{}, fmt.Errorf("octto: body required")
	}
	if len(in.Labels) > 20 {
		return Result{}, fmt.Errorf("octto: too many labels (max 20)")
	}
	// deterministic markdown
	var sbld strings.Builder
	sbld.WriteString("# ")
	sbld.WriteString(in.Title)
	sbld.WriteString("\n\n")
	sbld.WriteString(in.Body)
	sbld.WriteString("\n")
	if len(in.Labels) > 0 {
		sbld.WriteString("\nLabels: ")
		sbld.WriteString(strings.Join(in.Labels, ", "))
		sbld.WriteString("\n")
	}
	out := map[string]string{"markdown": sbld.String()}
	b, err := json.Marshal(out)
	if err != nil {
		return Result{}, fmt.Errorf("marshal error: %w", err)
	}
	return Result{Output: string(b)}, nil
}
