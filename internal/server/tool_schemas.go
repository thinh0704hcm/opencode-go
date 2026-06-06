package server

import "github.com/opencode-go/opencode-go/internal/provider"

// builtinToolSchemas maps known tool names to their provider-visible JSON-schema
// parameters and human descriptions. The model needs these to call tools
// correctly; an empty description/schema makes a generic model treat tool names
// as opaque and chat instead of executing.
var builtinToolSchemas = map[string]provider.ToolSchema{
	"bash": {
		Name:        "bash",
		Description: "Execute a bash command in the workspace. Use for git, build, running programs. Prefer read/write/edit/ls/glob/grep for file ops.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "The command to execute"},
			},
			"required": []string{"command"},
		},
	},
	"read": {
		Name:        "read",
		Description: "Read a file's contents.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File path relative to workspace"},
			},
			"required": []string{"path"},
		},
	},
	"write": {
		Name:        "write",
		Description: "Write/overwrite a file.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required": []string{"path", "content"},
		},
	},
	"edit": {
		Name:        "edit",
		Description: "Replace an exact string in a file.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
				"old":  map[string]any{"type": "string"},
				"new":  map[string]any{"type": "string"},
			},
			"required": []string{"path", "old", "new"},
		},
	},
	"ls": {
		Name:        "ls",
		Description: "List files in a directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
		},
	},
	"glob": {
		Name:        "glob",
		Description: "Find files matching a glob pattern.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string"},
			},
			"required": []string{"pattern"},
		},
	},
	"grep": {
		Name:        "grep",
		Description: "Search file contents by regex.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string"},
				"path":    map[string]any{"type": "string"},
			},
			"required": []string{"pattern"},
		},
	},
}

// schemaForTool returns the provider.ToolSchema for a known tool name, or a
// generic empty-object schema for unknown tools.
func schemaForTool(name string) provider.ToolSchema {
	if s, ok := builtinToolSchemas[name]; ok {
		return s
	}
	return provider.ToolSchema{
		Name:        name,
		Description: "",
		Parameters:  map[string]any{"type": "object"},
	}
}
