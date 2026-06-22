package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// Client is a minimal MCP client over the stdio transport. It spawns a local
// MCP server process and exchanges newline-delimited JSON-RPC 2.0 messages.
// Requests are issued one at a time (guarded by a mutex); the read side skips
// any interleaved notifications/log lines until the matching response id
// arrives. Not designed for concurrent in-flight requests.
type StdioClient struct {
    name   string
    cmd    *exec.Cmd
    stdin  io.WriteCloser
    stdout *bufio.Reader
    cancel context.CancelFunc

    mu     sync.Mutex
    nextID int64
    closed bool
    tools  []ToolDef
    onToolsChanged func()
    onClose        func(error)
}

// NewClient spawns the given command (argv) with extra env (KEY=VALUE strings
// appended to the current environment) and performs the MCP initialize
// handshake. name is the configured server name (for errors/logging). The
// returned Client is ready for ListTools/CallTool. Caller must Close() it.
func NewStdioClient(name string, argv []string, env []string) (*StdioClient, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("mcp %q: empty command", name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("mcp %q: stdin: %w", name, err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("mcp %q: stdout: %w", name, err)
	}
	// Stderr is left attached to the parent's stderr for diagnostics.
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("mcp %q: start: %w", name, err)
	}
	c := &StdioClient{
		name:   name,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReaderSize(stdoutPipe, 64*1024),
		cancel: cancel,
	}
	if err := c.Initialize(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// initialize performs the MCP initialize request + initialized notification.
func (c *StdioClient) Initialize() error {
	params := initializeParams{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo:      clientInfo{Name: "opencode-go", Version: "1.16.0"},
	}
	if _, err := c.call("initialize", params); err != nil {
		return fmt.Errorf("mcp %q: initialize: %w", c.name, err)
	}
	// Notify initialized (no id, no response expected).
	note := rpcRequest{JSONRPC: jsonRPCVersion, Method: "notifications/initialized"}
	c.mu.Lock()
	err := writeMessage(c.stdin, note)
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("mcp %q: initialized notify: %w", c.name, err)
	}
	return nil
}

// call issues a request and returns the matching response's Result, skipping
// interleaved notifications (responses with a nil id or a non-matching id).
func (c *StdioClient) call(method string, params any) (json.RawMessage, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.closed {
        return nil, fmt.Errorf("mcp %q: client closed", c.name)
    }
    c.nextID++
    id := c.nextID
    req := rpcRequest{JSONRPC: jsonRPCVersion, ID: &id, Method: method, Params: params}
    if err := writeMessage(c.stdin, req); err != nil {
        return nil, err
    }
    for {
        resp, err := readMessage(c.stdout)
        if err != nil {
            // notify close on read error
            if c.onClose != nil {
                c.onClose(err)
            }
            return nil, err
        }
        // Notification handling
        if resp.ID == nil {
            if resp.Method == NotificationToolsListChanged && c.onToolsChanged != nil {
                go c.onToolsChanged()
            }
            continue
        }
        if *resp.ID != id {
            continue
        }
        if resp.Error != nil {
            return nil, resp.Error
        }
        return resp.Result, nil
    }
}

// ListTools fetches and caches the server's advertised tools.
func (c *StdioClient) ListTools() ([]ToolDef, error) {
	raw, err := c.call("tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("mcp %q: tools/list: %w", c.name, err)
	}
	var res toolsListResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("mcp %q: tools/list decode: %w", c.name, err)
	}
	c.mu.Lock()
	c.tools = res.Tools
	c.mu.Unlock()
	return res.Tools, nil
}

// CallTool invokes a tool by its (bare, server-side) name with JSON arguments
// and returns the flattened text result. isError reflects the MCP isError flag.
func (c *StdioClient) CallTool(toolName string, args json.RawMessage) (text string, isError bool, err error) {
	raw, cerr := c.call("tools/call", toolsCallParams{Name: toolName, Arguments: args})
	if cerr != nil {
		return "", true, fmt.Errorf("mcp %q: tools/call %s: %w", c.name, toolName, cerr)
	}
	var res toolsCallResult
	if uerr := json.Unmarshal(raw, &res); uerr != nil {
		return "", true, fmt.Errorf("mcp %q: tools/call decode: %w", c.name, uerr)
	}
	return res.Text(), res.IsError, nil
}

// Name returns the configured server name.
func (c *StdioClient) Name() string { return c.name }

// Close terminates the server process and releases resources.
func (c *StdioClient) Close() error {
    c.mu.Lock()
    if c.closed {
        c.mu.Unlock()
        return nil
    }
    c.closed = true
    c.mu.Unlock()
    if c.stdin != nil {
        _ = c.stdin.Close()
    }
    if c.cancel != nil {
        c.cancel()
    }
    if c.cmd != nil {
        _ = c.cmd.Wait()
    c.notifyClosed(nil)
    }
    return nil
}

func (c *StdioClient) OnToolsChanged(fn func()) { c.onToolsChanged = fn }
func (c *StdioClient) OnClose(fn func(error)) { c.onClose = fn }

func (c *StdioClient) notifyToolsChanged() {
    if c.onToolsChanged != nil {
        c.onToolsChanged()
    }
}

func (c *StdioClient) notifyClosed(err error) {
    if c.onClose != nil {
        c.onClose(err)
    }
}


// ListPrompts stub – not supported in stdio client.
func (c *StdioClient) ListPrompts() ([]PromptDef, error) {
    return nil, fmt.Errorf("prompts not supported")
}

// GetPrompt stub – not supported in stdio client.
func (c *StdioClient) GetPrompt(name string, args map[string]string) (*PromptResult, error) {
    return nil, fmt.Errorf("prompts not supported")
}

// ListResources stub – not supported in stdio client.
func (c *StdioClient) ListResources() ([]ResourceDef, error) {
    return nil, fmt.Errorf("resources not supported")
}

// ReadResource stub – not supported in stdio client.
func (c *StdioClient) ReadResource(uri string) (*ResourceContent, error) {
    return nil, fmt.Errorf("resources not supported")
}
