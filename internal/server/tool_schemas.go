package server

import "github.com/opencode-go/opencode-go/internal/provider"

// builtinToolSchemas maps known tool names to their provider-visible JSON-schema
// parameters and human descriptions. The model needs these to call tools
// correctly; an empty description/schema makes a generic model treat tool names
// as opaque and chat instead of executing.
var builtinToolSchemas = map[string]provider.ToolSchema{

	"pty_spawn": {
		Name:        "pty_spawn",
		Description: "Spawn a long-running command in a pseudo-terminal. Returns a PTY id for later pty_read/pty_write/pty_kill.",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}, "args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "title": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}, "workdir": map[string]any{"type": "string"}, "timeoutSeconds": map[string]any{"type": "integer"}}, "required": []string{"command"}},
	},
	"pty_write": {
		Name:        "pty_write",
		Description: "Write raw input to a PTY session. Use data like \"\\x03\" for Ctrl+C or include newlines to submit commands.",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "data": map[string]any{"type": "string"}}, "required": []string{"id", "data"}},
	},
	"pty_read": {
		Name:        "pty_read",
		Description: "Read buffered PTY output lines, optionally filtered by regex pattern.",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "offset": map[string]any{"type": "integer"}, "limit": map[string]any{"type": "integer"}, "pattern": map[string]any{"type": "string"}, "ignoreCase": map[string]any{"type": "boolean"}}, "required": []string{"id"}},
	},
	"pty_list": {
		Name:        "pty_list",
		Description: "List active PTY sessions.",
		Parameters:  map[string]any{"type": "object"},
	},
	"pty_kill": {
		Name:        "pty_kill",
		Description: "Terminate a PTY session.",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "cleanup": map[string]any{"type": "boolean"}}, "required": []string{"id"}},
	},

	"delegate": {
		Name:        "delegate",
		Description: "Delegate a bounded subtask to a specialized subagent and return its result. Use for parallel research, review, planning, or focused analysis.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt":      map[string]any{"type": "string", "description": "Detailed subtask prompt"},
				"description": map[string]any{"type": "string", "description": "Short task description, used if prompt is empty"},
				"agent":       map[string]any{"type": "string", "description": "Optional agent name, e.g. security-auditor, plan, researcher"},
				"model":       map[string]any{"type": "string", "description": "Optional provider/model or model id"},
			},
		},
	},
	"task": {
		Name:        "task",
		Description: "Run a bounded subtask with an optional specialized agent and return the result. Alias of delegate for OpenCode Task compatibility.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt":      map[string]any{"type": "string", "description": "Detailed task prompt"},
				"description": map[string]any{"type": "string", "description": "Short task description, used if prompt is empty"},
				"agent":       map[string]any{"type": "string", "description": "Optional agent name"},
				"model":       map[string]any{"type": "string", "description": "Optional provider/model or model id"},
			},
		},
	},
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

	"plan_save": {
		Name:        "plan_save",
		Description: "Save the current implementation plan for this workspace.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{"type": "string"},
			},
			"required": []string{"content"},
		},
	},
	"plan_read": {
		Name:        "plan_read",
		Description: "Read the saved implementation plan for this workspace.",
		Parameters:  map[string]any{"type": "object"},
	},
	"worktree_create": {
		Name:        "worktree_create",
		Description: "Create a git worktree under ~/.local/share/opencode/worktrees for a new branch.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"branch":     map[string]any{"type": "string"},
				"baseBranch": map[string]any{"type": "string"},
			},
			"required": []string{"branch"},
		},
	},
	"worktree_delete": {
		Name:        "worktree_delete",
		Description: "Remove a git worktree previously created under ~/.local/share/opencode/worktrees.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":  map[string]any{"type": "string"},
				"force": map[string]any{"type": "boolean"},
			},
			"required": []string{"path"},
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
	"webfetch": {
		Name:        "webfetch",
		Description: "Fetch the content of a URL and return its text.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":    map[string]any{"type": "string", "description": "URL to fetch"},
				"prompt": map[string]any{"type": "string", "description": "What to look for in the page"},
			},
			"required": []string{"url"},
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
