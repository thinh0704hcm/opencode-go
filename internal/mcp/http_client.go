package mcp

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"
    "sync/atomic"
)

// HTTPClient implements MCPClient for remote HTTP/SSE transports.
type HTTPClient struct {
    name    string
    url     string
    client  *http.Client
    headers map[string]string
    id      atomic.Int64
}

// NewHTTPClient creates a new HTTP-based MCP client.
func NewHTTPClient(name, url string, headers map[string]string, timeout time.Duration) *HTTPClient {
    if timeout <= 0 {
        timeout = 30 * time.Second
    }
    return &HTTPClient{
        name:    name,
        url:     url,
        client:  &http.Client{Timeout: timeout},
        headers: headers,
    }
}

func (c *HTTPClient) Name() string { return c.name }

func (c *HTTPClient) Initialize() error {
    _, err := c.call("initialize", initializeParams{
        ProtocolVersion: ProtocolVersion,
        Capabilities:    map[string]any{},
        ClientInfo:      clientInfo{Name: "opencode-go", Version: "1.16.0"},
    })
    return err
}

func (c *HTTPClient) ListTools() ([]ToolDef, error) {
    raw, err := c.call("tools/list", map[string]any{})
    if err != nil {
        return nil, err
    }
    var res toolsListResult
    if err := json.Unmarshal(raw, &res); err != nil {
        return nil, fmt.Errorf("mcp %q: tools/list decode: %w", c.name, err)
    }
    return res.Tools, nil
}

func (c *HTTPClient) CallTool(name string, args json.RawMessage) (string, bool, error) {
    raw, err := c.call("tools/call", toolsCallParams{Name: name, Arguments: args})
    if err != nil {
        return "", true, err
    }
    var res toolsCallResult
    if err := json.Unmarshal(raw, &res); err != nil {
        return "", true, fmt.Errorf("mcp %q: tools/call decode: %w", c.name, err)
    }
    return res.Text(), res.IsError, nil
}

func (c *HTTPClient) ListPrompts() ([]PromptDef, error) {
    raw, err := c.call("prompts/list", map[string]any{})
    if err != nil {
        return nil, err
    }
    var res struct{ Prompts []PromptDef `json:"prompts"` }
    if err := json.Unmarshal(raw, &res); err != nil {
        return nil, fmt.Errorf("mcp %q: prompts/list decode: %w", c.name, err)
    }
    return res.Prompts, nil
}

func (c *HTTPClient) GetPrompt(name string, args map[string]string) (*PromptResult, error) {
    raw, err := c.call("prompts/get", struct {
        Name      string            `json:"name"`
        Arguments map[string]string `json:"arguments,omitempty"`
    }{Name: name, Arguments: args})
    if err != nil {
        return nil, err
    }
    var res PromptResult
    if err := json.Unmarshal(raw, &res); err != nil {
        return nil, fmt.Errorf("mcp %q: prompts/get decode: %w", c.name, err)
    }
    return &res, nil
}

func (c *HTTPClient) ListResources() ([]ResourceDef, error) {
    raw, err := c.call("resources/list", map[string]any{})
    if err != nil {
        return nil, err
    }
    var res struct{ Resources []ResourceDef `json:"resources"` }
    if err := json.Unmarshal(raw, &res); err != nil {
        return nil, fmt.Errorf("mcp %q: resources/list decode: %w", c.name, err)
    }
    return res.Resources, nil
}

func (c *HTTPClient) ReadResource(uri string) (*ResourceContent, error) {
    raw, err := c.call("resources/read", struct{ URI string `json:"uri"` }{URI: uri})
    if err != nil {
        return nil, err
    }
    var res struct {
        Contents []ResourceContent `json:"contents"`
    }
    if err := json.Unmarshal(raw, &res); err != nil {
        return nil, fmt.Errorf("mcp %q: resources/read decode: %w", c.name, err)
    }
    if len(res.Contents) == 0 {
        return nil, fmt.Errorf("mcp %q: resource not found: %s", c.name, uri)
    }
    return &res.Contents[0], nil
}

func (c *HTTPClient) Close() error { return nil }

func (c *HTTPClient) OnToolsChanged(fn func()) {}
func (c *HTTPClient) OnClose(fn func(error)) {}

func (c *HTTPClient) call(method string, params any) (json.RawMessage, error) {
    req := rpcRequest{JSONRPC: jsonRPCVersion, ID: ptrInt64(c.id.Add(1)), Method: method, Params: params}
    data, err := json.Marshal(req)
    if err != nil {
        return nil, err
    }
    httpReq, err := http.NewRequest("POST", c.url, bytes.NewReader(data))
    if err != nil {
        return nil, err
    }
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("Accept", "application/json, text/event-stream")
    for k, v := range c.headers {
        httpReq.Header.Set(k, v)
    }
    resp, err := c.client.Do(httpReq)
    if err != nil {
        return nil, fmt.Errorf("mcp %q: %s: %w", c.name, method, err)
    }
    defer resp.Body.Close()
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("mcp %q: %s read body: %w", c.name, method, err)
    }

    if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
        if c.headers == nil {
            c.headers = make(map[string]string)
        }
        c.headers["MCP-Session-Id"] = sid
    }

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("mcp %q: %s: HTTP %d: %s", c.name, method, resp.StatusCode, body)
    }
    var rpcResp rpcResponse
    if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
        if idx := bytes.Index(body, []byte("data: ")); idx != -1 {
            body = body[idx+6:]
        }
    }

    // fmt.Printf("DEBUG mcp %s: %s\n", c.name, string(body))
    if err := json.Unmarshal(body, &rpcResp); err != nil {
        return nil, fmt.Errorf("mcp %q: %s decode: %w", c.name, method, err)
    }
    if rpcResp.Error != nil {
        return nil, rpcResp.Error
    }
    return rpcResp.Result, nil
}

func ptrInt64(v int64) *int64 { return &v }
