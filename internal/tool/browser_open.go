package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	maxBrowserURLLen  = 4096
	maxBrowserTextLen = 8 * 1024
	maxBrowserCodeLen = 64 * 1024
)

type browserOpenTool struct{}
type browserSessionTool struct{}

type browserControlInput struct {
	Action    string `json:"action,omitempty"`
	Mode      string `json:"mode,omitempty"`
	URL       string `json:"url,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Selector  string `json:"selector,omitempty"`
	Script    string `json:"script,omitempty"`
	Text      string `json:"text,omitempty"`
	X         *int   `json:"x,omitempty"`
	Y         *int   `json:"y,omitempty"`
	Timeout   *int   `json:"timeout,omitempty"`
}

func (browserOpenTool) Name() string   { return "browser_open" }
func (browserOpenTool) Mutating() bool { return false }

func (browserSessionTool) Name() string   { return "browser_session" }
func (browserSessionTool) Mutating() bool { return false }

func (browserOpenTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in browserControlInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	action := browserAction(in)
	if action == "" {
		action = "open"
	}
	return executeBrowserAction(ctx, in, action, true)
}

func (browserSessionTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in browserControlInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	action := browserAction(in)
	if action == "" {
		action = "status"
	}
	switch action {
	case "status", "navigate", "text", "screenshot", "click", "type", "eval", "close":
		return executeBrowserAction(ctx, in, action, false)
	default:
		return Result{}, fmt.Errorf("unsupported action %s", action)
	}
}

func browserAction(in browserControlInput) string {
	if in.Action != "" {
		return strings.ToLower(in.Action)
	}
	return strings.ToLower(in.Mode)
}

func executeBrowserAction(ctx context.Context, in browserControlInput, action string, allowOpenFetch bool) (Result, error) {
	if err := validateBrowserInput(in, action); err != nil {
		return Result{}, err
	}
	var parsedURL string
	if in.URL != "" {
		parsed, err := validateHTTPURL(in.URL, "url")
		if err != nil {
			return Result{}, err
		}
		parsedURL = parsed.String()
	}
	switch action {
	case "open":
		if !allowOpenFetch {
			return Result{}, fmt.Errorf("unsupported action %s", action)
		}
		return browserOpen(parsedURL)
	case "fetch":
		if !allowOpenFetch {
			return Result{}, fmt.Errorf("unsupported action %s", action)
		}
		return browserFetch(ctx, parsedURL)
	case "status", "navigate", "text", "screenshot", "click", "type", "eval", "close":
		return browserControl(ctx, in, action, parsedURL)
	default:
		return Result{}, fmt.Errorf("unsupported mode %s", action)
	}
}

func validateBrowserInput(in browserControlInput, action string) error {
	switch action {
	case "open", "fetch", "navigate":
		if in.URL == "" {
			return fmt.Errorf("url required for %s", action)
		}
	}
	if len(in.Selector) > maxBrowserTextLen {
		return fmt.Errorf("selector exceeds 8KiB")
	}
	if len(in.Text) > maxBrowserTextLen {
		return fmt.Errorf("text exceeds 8KiB")
	}
	if len(in.Script) > maxBrowserCodeLen {
		return fmt.Errorf("script exceeds 64KiB")
	}
	return nil
}

func validateHTTPURL(raw, name string) (*url.URL, error) {
	if len(raw) > maxBrowserURLLen {
		return nil, fmt.Errorf("%s exceeds 4096 bytes", name)
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("%s must be a valid http or https URL", name)
	}
	return parsed, nil
}

func browserOpen(parsedURL string) (Result, error) {
	if os.Getenv("ALLOW_BROWSER_OPEN") != "1" {
		return Result{Output: "noop"}, nil
	}
	ctxCmd, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctxCmd, "open", parsedURL)
	case "windows":
		cmd = exec.CommandContext(ctxCmd, "rundll32", "url.dll,FileProtocolHandler", parsedURL)
	default:
		cmd = exec.CommandContext(ctxCmd, "xdg-open", parsedURL)
	}
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	if err := cmd.Wait(); err != nil {
		return Result{}, fmt.Errorf("browser open failed: %w", err)
	}
	return Result{Output: "opened"}, nil
}

func browserFetch(ctx context.Context, parsedURL string) (Result, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", "opencode-go/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("fetch failed: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return Result{}, err
	}
	return Result{Output: string(body)}, nil
}

func browserControl(ctx context.Context, in browserControlInput, action, parsedURL string) (Result, error) {
	endpoint := os.Getenv("BROWSER_CONTROL_ENDPOINT")
	if endpoint == "" {
		return Result{}, fmt.Errorf("BROWSER_CONTROL_ENDPOINT not set for %s", action)
	}
	parsedEndpoint, err := validateHTTPURL(endpoint, "BROWSER_CONTROL_ENDPOINT")
	if err != nil {
		return Result{}, err
	}

	payload := map[string]any{"action": action}
	if parsedURL != "" {
		payload["url"] = parsedURL
	}
	if in.SessionID != "" {
		payload["session_id"] = in.SessionID
	}
	if in.Selector != "" {
		payload["selector"] = in.Selector
	}
	if in.Script != "" {
		payload["script"] = in.Script
	}
	if in.Text != "" {
		payload["text"] = in.Text
	}
	if in.X != nil {
		payload["x"] = *in.X
	}
	if in.Y != nil {
		payload["y"] = *in.Y
	}
	if in.Timeout != nil {
		payload["timeout"] = *in.Timeout
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	method := http.MethodPost
	var reader io.Reader = bytes.NewReader(body)
	if action == "status" {
		method = http.MethodGet
		reader = nil
	}
	req, err := http.NewRequestWithContext(ctx, method, parsedEndpoint.String(), reader)
	if err != nil {
		return Result{}, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("%s endpoint error: %s", action, resp.Status)
	}
	out, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return Result{}, err
	}
	return Result{Output: string(out)}, nil
}
