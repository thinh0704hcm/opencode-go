package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/opencode-go/opencode-go/internal/provider"
)

func TestToolAdapter(t *testing.T) {
	py := skipIfNoPython(t)
	c, err := NewClient("mock", []string{py, "-c", mockServerScript}, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()
	defs, err := c.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	adapters := NewToolAdapters(c, defs)
	if len(adapters) != 1 {
		t.Fatalf("want 1 adapter, got %d", len(adapters))
	}
	a := adapters[0]
	if a.Name() != "mock_echo" {
		t.Fatalf("name = %q, want mock_echo", a.Name())
	}
	if !a.Mutating() {
		t.Fatal("MCP tools must be permission-gated (Mutating=true)")
	}
	args, _ := json.Marshal(map[string]any{"k": "v"})
	res, err := a.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Output == "" {
		t.Fatal("empty output")
	}
	// Schema() must surface the tool under its namespaced name.
	type hasSchema interface {
		Schema() provider.ToolSchema
	}
	sc, ok := a.(hasSchema)
	if !ok {
		t.Fatal("adapter does not implement Schema()")
	}
	if sc.Schema().Name != "mock_echo" {
		t.Fatalf("schema name = %q", sc.Schema().Name)
	}
}
