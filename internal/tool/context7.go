package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

type context7Tool struct{}

func (context7Tool) Name() string   { return "context7" }
func (context7Tool) Mutating() bool { return false }

func (context7Tool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Package string `json:"package"`
		Query   string `json:"query"`
		Version string `json:"version,omitempty"`
		Remote  bool   `json:"remote,omitempty"`
		Mode    string `json:"mode,omitempty"`
		Server  string `json:"server,omitempty"`
		Tool    string `json:"tool,omitempty"`
		Timeout int    `json:"timeout,omitempty"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.Package) == "" {
		return Result{}, fmt.Errorf("package must be provided")
	}
	cleanComp := func(s string) string { return strings.Trim(path.Clean("/"+s), "/") }
	pkg := cleanComp(in.Package)
	ver := cleanComp(in.Version)
	qry := cleanComp(in.Query)

	deterministicHost := "docs.context7.com"
	u := url.URL{Scheme: "https", Host: deterministicHost}
	if ver != "" {
		u.Path = path.Join(pkg, ver, qry)
	} else {
		u.Path = path.Join(pkg, qry)
	}

	// Determine mode
	mode := strings.ToLower(in.Mode)
	if mode == "" {
		if in.Remote {
			mode = "http"
		} else {
			mode = "local"
		}
	}

	switch mode {
	case "local":
		plan := fmt.Sprintf("Fetch %s", u.String())
		outBytes, err := json.Marshal(map[string]string{"url": u.String(), "plan": plan, "mode": "local"})
		if err != nil {
			return Result{}, err
		}
		return Result{Output: string(outBytes)}, nil
	case "http":
		base := os.Getenv("CONTEXT7_BASE_URL")
		if base == "" {
			return Result{}, fmt.Errorf("CONTEXT7_BASE_URL not set for http mode")
		}
		if _, err := url.Parse(base); err != nil {
			return Result{}, fmt.Errorf("invalid CONTEXT7_BASE_URL: %s", base)
		}
		remoteURL := fmt.Sprintf("%s/docs?package=%s&version=%s&query=%s", strings.TrimRight(base, "/"), url.QueryEscape(in.Package), url.QueryEscape(in.Version), url.QueryEscape(in.Query))
		// timeout
		timeoutSec := 10
		if in.Timeout > 0 {
			if in.Timeout < 1 {
				timeoutSec = 1
			} else if in.Timeout > 30 {
				timeoutSec = 30
			} else {
				timeoutSec = in.Timeout
			}
		} else if tsStr := os.Getenv("CONTEXT7_TIMEOUT"); tsStr != "" {
			if v, err := strconv.Atoi(tsStr); err == nil {
				if v < 1 {
					v = 1
				} else if v > 30 {
					v = 30
				}
				timeoutSec = v
			}
		}
		ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
		client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
		if err != nil {
			return Result{}, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return Result{}, err
		}
		defer resp.Body.Close()
		limited := io.LimitReader(resp.Body, 64*1024)
		body, err := io.ReadAll(limited)
		if err != nil {
			return Result{}, err
		}
		outMap := map[string]any{"url": remoteURL, "content": string(body), "mode": "remote", "status": resp.Status}
		outBytes, err := json.Marshal(outMap)
		if err != nil {
			return Result{}, err
		}
		return Result{Output: string(outBytes)}, nil
	case "mcp":
		// Resolve endpoint
		endpoint := in.Server
		if endpoint == "" {
			endpoint = os.Getenv("CONTEXT7_MCP_URL")
		}
		if endpoint == "" {
			return Result{}, fmt.Errorf("context7 mcp not configured")
		}
		// Validate scheme
		if u, err := url.Parse(endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return Result{}, fmt.Errorf("invalid CONTEXT7_MCP_URL: %s", endpoint)
		}
		// Determine tool name
		toolName := in.Tool
		if toolName == "" {
			toolName = "get-library-docs"
		}
		// Build JSON-RPC payload
		payload := map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"params": map[string]any{
				"name": toolName,
				"arguments": map[string]string{
					"package": in.Package,
					"version": in.Version,
					"query":   in.Query,
				},
			},
			"id": 1,
		}
		b, _ := json.Marshal(payload)
		// Timeout handling (input overrides env)
		timeoutSec := 10
		if in.Timeout > 0 {
			if in.Timeout < 1 {
				timeoutSec = 1
			} else if in.Timeout > 30 {
				timeoutSec = 30
			} else {
				timeoutSec = in.Timeout
			}
		} else if tsStr := os.Getenv("CONTEXT7_TIMEOUT"); tsStr != "" {
			if v, err := strconv.Atoi(tsStr); err == nil {
				if v < 1 {
					v = 1
				} else if v > 30 {
					v = 30
				}
				timeoutSec = v
			}
		}
		ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(b)))
		if err != nil {
			return Result{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		if token := os.Getenv("CONTEXT7_MCP_AUTH"); token != "" {
			if strings.HasPrefix(token, "Bearer ") {
				req.Header.Set("Authorization", token)
			} else {
				req.Header.Set("Authorization", "Bearer "+token)
			}
		}
		client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return Result{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return Result{}, fmt.Errorf("MCP request failed with status %d", resp.StatusCode)
		}
		limited := io.LimitReader(resp.Body, 64*1024)
		body, err := io.ReadAll(limited)
		if err != nil {
			return Result{}, err
		}
		// Parse response JSON
		var respObj map[string]any
		if err := json.Unmarshal(body, &respObj); err != nil {
			return Result{}, fmt.Errorf("invalid MCP JSON response")
		}

		// Validation logic
		if version, _ := respObj["jsonrpc"].(string); version != "2.0" {
			return Result{}, fmt.Errorf("invalid MCP jsonrpc version")
		}
		if errObj, ok := respObj["error"].(map[string]any); ok {
			code := 0
			if c, ok := errObj["code"].(float64); ok {
				code = int(c)
			}
			message, _ := errObj["message"].(string)
			if message == "" {
				message = "MCP error"
			}
			return Result{}, fmt.Errorf("MCP error %d: %s", code, message)
		}

		flatten := func(v any) string {
			switch val := v.(type) {
			case string:
				return val
			case []any:
				var parts []string
				for _, item := range val {
					if m, ok := item.(map[string]any); ok {
						if txt, ok := m["text"].(string); ok {
							parts = append(parts, txt)
						}
					}
				}
				return strings.Join(parts, " ")
			default:
				return ""
			}
		}
		var content string
		if res, ok := respObj["result"].(map[string]any); ok {
			if isErr, ok := res["isError"].(bool); ok && isErr {
				content = flatten(res["content"])
				if content == "" {
					content = "MCP error"
				}
				return Result{}, fmt.Errorf(content)
			}
			content = flatten(res["content"])
		} else if c, ok := respObj["content"]; ok {
			content = flatten(c)
		} else {
			content = string(body)
		}
		outMap := map[string]any{"mode": "mcp", "url": endpoint, "tool": toolName, "content": content, "status": "ok"}
		outBytes, err := json.Marshal(outMap)
		if err != nil {
			return Result{}, err
		}
		return Result{Output: string(outBytes)}, nil
	default:
		return Result{}, fmt.Errorf("invalid mode %s", mode)
	}

}
