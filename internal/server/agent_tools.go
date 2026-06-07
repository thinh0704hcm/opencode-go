package server

import (
	"context"

	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/tool"
)

// schemaer is implemented by tools (e.g. MCP adapters) that carry their own
// provider schema; builtins fall back to the static schemaForTool table.
type schemaer interface {
	Schema() provider.ToolSchema
}

// toolSchemas maps every registered tool to a provider.ToolSchema the model can
// see, attaching real descriptions and JSON-schema parameters for known tools.
func toolSchemas(reg *tool.Registry, allow func(string) bool) []provider.ToolSchema {
	tools := reg.List()
	schemas := make([]provider.ToolSchema, 0, len(tools))
	for _, t := range tools {
		if allow != nil && !allow(t.Name()) {
			continue
		}
		if sc, ok := t.(schemaer); ok {
			schemas = append(schemas, sc.Schema())
			continue
		}
		schemas = append(schemas, schemaForTool(t.Name()))
	}
	return schemas
}

// executeToolCall runs a model-issued tool call against the sandbox. It does NOT
// gate on permission; the agent loop performs gating before calling this.
// Returns the tool output (or error text) and whether the result is an error.
func executeToolCall(ctx context.Context, reg *tool.Registry, sb *tool.Sandbox, call provider.ToolCall) (output string, isError bool) {
	t, ok := reg.Lookup(call.Name)
	if !ok {
		return "unknown tool: " + call.Name, true
	}
	res, err := t.Execute(ctx, call.Input, sb)
	if err != nil {
		return err.Error(), true
	}
	return res.Output, false
}

// needsPermission reports whether the named tool is mutating and therefore
// requires a permission grant before execution. Unknown tools return false.
func needsPermission(reg *tool.Registry, name string) bool {
	t, ok := reg.Lookup(name)
	if !ok {
		return false
	}
	return t.Mutating()
}
