package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type memoryTool struct{}

func (memoryTool) Name() string   { return "memory" }
func (memoryTool) Mutating() bool { return true }

func (memoryTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Action    string `json:"action"`
		Scope     string `json:"scope"`
		Key       string `json:"key,omitempty"`
		Value     string `json:"value,omitempty"`
		Query     string `json:"query,omitempty"`
		Remote    bool   `json:"remote,omitempty"`
		Limit     int    `json:"limit,omitempty"`
		Timestamp int64  `json:"timestamp,omitempty"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	// Default scope project, validate enum
	if in.Scope == "" {
		in.Scope = "project"
	}
	if in.Scope != "project" && in.Scope != "user" {
		return Result{}, fmt.Errorf("invalid scope: %s", in.Scope)
	}
	// Determine file path based on scope
	var path string
	if in.Scope == "project" {
		// Resolve sandbox root safely
		root := ""
		if sb != nil {
			root = sb.Root()
		}
		if root == "" {
			// fallback to current directory
			root, _ = os.Getwd()
		}
		// Clean and ensure within root
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return Result{}, err
		}
		path = filepath.Join(absRoot, ".opencode_memory_project.jsonl")
	} else {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".opencode_memory_user.jsonl")
	}
	// Ensure directory exists with restrictive perms
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, err
	}

	// Remote mode: real HTTP adapter when env vars set
	// Validate base URL scheme and use configurable timeout (env SUPERMEMORY_TIMEOUT seconds, clamped 1-30, default 10)
	if in.Remote {
		apiKey := os.Getenv("SUPERMEMORY_API_KEY")
		baseURL := os.Getenv("SUPERMEMORY_BASE_URL")
		if apiKey != "" && baseURL != "" {
			// Validate base URL scheme
			if u, err := url.Parse(baseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				return Result{}, fmt.Errorf("invalid SUPERMEMORY_BASE_URL: %s", baseURL)
			}
			// Determine timeout from env, clamp 1-30 seconds, default 10
			timeoutSec := 10
			if tsStr := os.Getenv("SUPERMEMORY_TIMEOUT"); tsStr != "" {
				if v, err := strconv.Atoi(tsStr); err == nil {
					if v < 1 {
						v = 1
					} else if v > 30 {
						v = 30
					}
					timeoutSec = v
				}
			}

			// caps
			const maxKeyLen = 1024
			const maxValLen = 64 * 1024
			const maxQueryLen = 1024
			if len(in.Key) > maxKeyLen || len(in.Value) > maxValLen || len(in.Query) > maxQueryLen {
				return Result{}, fmt.Errorf("input exceeds size limits")
			}
			// context timeout
			ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
			defer cancel()
			client := &http.Client{}
			var req *http.Request
			var err error
			switch strings.ToLower(in.Action) {
			case "add":
				u := baseURL + "/v1/memories"
				body, _ := json.Marshal(map[string]string{"key": in.Key, "value": in.Value, "scope": in.Scope})
				req, err = http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(body)))
			case "search":
				u := fmt.Sprintf("%s/v1/memories/search?query=%s&limit=%d", baseURL, url.QueryEscape(in.Query), in.Limit)
				req, err = http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			case "list":
				u := fmt.Sprintf("%s/v1/memories?scope=%s&limit=%d", baseURL, url.QueryEscape(in.Scope), in.Limit)
				req, err = http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			case "forget":
				u := fmt.Sprintf("%s/v1/memories/%s", baseURL, url.PathEscape(in.Key))
				req, err = http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
			default:
				return Result{}, fmt.Errorf("unknown action %s", in.Action)
			}
			if err != nil {
				return Result{}, err
			}
			req.Header.Set("Authorization", "Bearer "+apiKey)
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				return Result{}, err
			}
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return Result{}, fmt.Errorf("remote error: %s", resp.Status)
			}
			switch strings.ToLower(in.Action) {
			case "add":
				return Result{Output: "added"}, nil
			case "forget":
				return Result{Output: "forgot"}, nil
			case "list", "search":
				limited := io.LimitReader(resp.Body, 64*1024)
				b, _ := io.ReadAll(limited)
				return Result{Output: string(b)}, nil
			}
		}
	}
	// Fallback to local file mode
	switch strings.ToLower(in.Action) {
	case "add":
		if in.Key == "" || in.Value == "" {
			return Result{}, fmt.Errorf("add requires key and value")
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return Result{}, err
		}
		// Ensure close error is captured
		defer func() {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}()
		entry := map[string]string{"key": in.Key, "value": in.Value}
		b, merr := json.Marshal(entry)
		if merr != nil {
			return Result{}, merr
		}
		if _, werr := f.Write(b); werr != nil {
			return Result{}, werr
		}
		if _, werr := f.Write([]byte("\n")); werr != nil {
			return Result{}, werr
		}
		if err != nil {
			return Result{}, err
		}
		return Result{Output: "added"}, nil
	case "list":
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return Result{Output: ""}, nil
			}
			return Result{}, err
		}
		// cap output size to 64KB to avoid huge payloads
		const maxOutput = 64 * 1024
		if len(data) > maxOutput {
			data = data[:maxOutput]
		}
		return Result{Output: string(data)}, nil
	case "search":
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				return Result{Output: ""}, nil
			}
			return Result{}, err
		}
		defer f.Close()
		var b strings.Builder
		scanner := bufio.NewScanner(f)
		const maxResults = 100
		count := 0
		for scanner.Scan() {
			if count >= maxResults {
				break
			}
			line := scanner.Text()
			var entry map[string]string
			if err := json.Unmarshal([]byte(line), &entry); err == nil {
				if in.Query != "" && strings.Contains(entry["value"], in.Query) {
					b.WriteString(line)
					b.WriteByte('\n')
					count++
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return Result{}, err
		}
		return Result{Output: b.String()}, nil
	case "forget":
		if in.Key == "" {
			return Result{}, fmt.Errorf("forget requires key")
		}
		// read all, filter out key
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				return Result{Output: ""}, nil
			}
			return Result{}, err
		}
		var entries []string
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			var entry map[string]string
			if err := json.Unmarshal([]byte(line), &entry); err == nil && entry["key"] != in.Key {
				entries = append(entries, line)
			}
		}
		if err := scanner.Err(); err != nil {
			f.Close()
			return Result{}, err
		}
		f.Close()
		// rewrite file
		out, err := os.Create(path)
		if err != nil {
			return Result{}, err
		}
		for _, l := range entries {
			if _, werr := out.WriteString(l); werr != nil {
				out.Close()
				return Result{}, werr
			}
			if _, werr := out.WriteString("\n"); werr != nil {
				out.Close()
				return Result{}, werr
			}
		}
		if cerr := out.Close(); cerr != nil {
			return Result{}, cerr
		}
		return Result{Output: "forgot"}, nil
	default:
		return Result{}, fmt.Errorf("unknown action %s", in.Action)
	}
}
