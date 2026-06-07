// Package mcp implements a minimal MCP (Model Context Protocol) client over the
// stdio transport: newline-delimited JSON-RPC 2.0. It supports the subset
// opencode-go needs — initialize, tools/list, tools/call — for consuming local
// MCP servers. Remote/SSE transports and OAuth are intentionally unimplemented.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// jsonRPCVersion is the only version this client speaks.
const jsonRPCVersion = "2.0"

// ProtocolVersion is the MCP protocol revision advertised in initialize.
const ProtocolVersion = "2024-11-05"

// rpcRequest is a JSON-RPC 2.0 request. ID is omitted for notifications.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("mcp rpc error %d: %s", e.Code, e.Message)
}

// --- initialize ---

// initializeParams is the params for the initialize request.
type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      clientInfo     `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// initializeResult is the server's initialize response (we only need to know it
// succeeded; fields are kept minimal).
type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

// --- tools/list ---

// ToolDef describes one tool advertised by an MCP server.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// toolsListResult is the tools/list response.
type toolsListResult struct {
	Tools []ToolDef `json:"tools"`
}

// --- tools/call ---

// toolsCallParams is the params for tools/call.
type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// toolsCallResult is the tools/call response. Content is a list of typed blocks;
// MCP text results use {type:"text", text:"..."}.
type toolsCallResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Text flattens the result content blocks into a single string (text blocks
// joined by newlines; non-text blocks are noted by type).
func (r toolsCallResult) Text() string {
	var out string
	for i, b := range r.Content {
		if i > 0 {
			out += "\n"
		}
		if b.Type == "text" {
			out += b.Text
		} else {
			out += "[" + b.Type + " content]"
		}
	}
	return out
}

// --- newline-delimited framing ---

// writeMessage marshals v to JSON and writes it as a single newline-terminated
// line (the MCP stdio framing: one JSON object per line, no embedded newlines).
func writeMessage(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// readMessage reads one newline-delimited JSON line and unmarshals it into a
// rpcResponse. Blank lines are skipped. Returns io.EOF when the stream closes.
func readMessage(r *bufio.Reader) (rpcResponse, error) {
	for {
		line, err := r.ReadBytes('\n')
		trimmed := trimSpace(line)
		if len(trimmed) > 0 {
			var resp rpcResponse
			if uerr := json.Unmarshal(trimmed, &resp); uerr != nil {
				return rpcResponse{}, fmt.Errorf("mcp: bad message: %w", uerr)
			}
			return resp, nil
		}
		if err != nil {
			return rpcResponse{}, err
		}
	}
}

// trimSpace trims leading/trailing ASCII whitespace without importing strings
// for a hot path.
func trimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}
