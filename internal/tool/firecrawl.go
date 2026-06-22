package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// firecrawlBaseURL is overridable in tests.
var firecrawlBaseURL = "https://api.firecrawl.dev"

// firecrawlHTTPClient is overridable in tests.
var firecrawlHTTPClient = &http.Client{Timeout: 30 * time.Second}

type firecrawlTool struct{}

func (firecrawlTool) Name() string   { return "firecrawl" }
func (firecrawlTool) Mutating() bool { return false }

func (firecrawlTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		URL       string `json:"url"`
		Prompt    string `json:"prompt,omitempty"`
		MaxTokens int    `json:"maxTokens,omitempty"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.URL) == "" {
		return Result{}, errors.New("firecrawl: url required")
	}
	apiKey := strings.TrimSpace(os.Getenv("FIRECRAWL_API_KEY"))
	if apiKey == "" {
		return Result{}, errors.New("firecrawl: missing FIRECRAWL_API_KEY; run `firecrawl login --browser` or set the env var")
	}
	payload := map[string]any{"url": in.URL, "formats": []string{"markdown"}}
	if in.Prompt != "" {
		payload["prompt"] = in.Prompt
	}
	if in.MaxTokens > 0 {
		payload["maxTokens"] = in.MaxTokens
	}
	bodyBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/v1/scrape", firecrawlBaseURL), strings.NewReader(string(bodyBytes)))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := firecrawlHTTPClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("firecrawl: HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	var parsed struct {
		Data struct {
			Markdown string `json:"markdown"`
		} `json:"data"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Result{}, err
	}
	markdown := parsed.Data.Markdown
	if markdown == "" {
		markdown = parsed.Markdown
	}
	out, truncated := TruncateOutput([]byte(markdown))
	return Result{Output: out, Truncated: truncated}, nil
}

// NewFirecrawlTool returns a firecrawl tool instance.
func NewFirecrawlTool() Tool { return firecrawlTool{} }
