package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/tool"
)

// toolAdapter exposes one MCP-server tool as a tool.Tool so the agent loop can
// schedule it like a builtin. It ignores the sandbox (MCP tools execute in the
// remote server process) and forwards the call over JSON-RPC. The registry key
// (Name) is namespaced "<server>_<tool>" to avoid collisions with builtins and
// other servers; the bare remote name is kept for the tools/call request.
type toolAdapter struct {
	client      *Client
	remoteName  string // bare tool name on the MCP server
	fullName    string // namespaced registry name
	desc        string
	inputSchema json.RawMessage
}

// NewToolAdapters builds a tool.Tool for each tool advertised by client.
// serverName namespaces the registry names.
func NewToolAdapters(client *Client, defs []ToolDef) []tool.Tool {
	out := make([]tool.Tool, 0, len(defs))
	for _, d := range defs {
		out = append(out, &toolAdapter{
			client:      client,
			remoteName:  d.Name,
			fullName:    client.Name() + "_" + d.Name,
			desc:        d.Description,
			inputSchema: d.InputSchema,
		})
	}
	return out
}

// Name returns the namespaced tool name.
func (a *toolAdapter) Name() string { return a.fullName }

// Mutating reports true: MCP tools have no reliable mutability signal in the
// protocol, so every MCP call is permission-gated for safety.
func (a *toolAdapter) Mutating() bool { return true }

// Execute forwards the call to the MCP server over JSON-RPC. The sandbox is
// unused (the remote process performs the work). An MCP isError result is
// surfaced as a Go error so the agent loop records a failed tool part.
func (a *toolAdapter) Execute(ctx context.Context, input json.RawMessage, _ *tool.Sandbox) (tool.Result, error) {
	if len(input) == 0 {
		input = json.RawMessage("{}")
	}
	text, isErr, err := a.client.CallTool(a.remoteName, input)
	if err != nil {
		return tool.Result{}, err
	}
	if isErr {
		if text == "" {
			text = fmt.Sprintf("mcp tool %q returned an error", a.fullName)
		}
		return tool.Result{Output: text}, fmt.Errorf("%s", text)
	}
	return tool.Result{Output: text}, nil
}

// Schema returns the provider tool schema for this MCP tool, mapping the
// server's advertised JSON input schema into the provider's Parameters shape.
// Implements the server package's Schemaer seam (duck-typed). Falls back to a
// permissive empty object schema when the server omits/!malforms inputSchema.
func (a *toolAdapter) Schema() provider.ToolSchema {
	params := map[string]any{"type": "object", "properties": map[string]any{}}
	if len(a.inputSchema) > 0 {
		var parsed map[string]any
		if err := json.Unmarshal(a.inputSchema, &parsed); err == nil && len(parsed) > 0 {
			params = parsed
		}
	}
	return provider.ToolSchema{
		Name:        a.fullName,
		Description: a.desc,
		Parameters:  params,
	}
}
