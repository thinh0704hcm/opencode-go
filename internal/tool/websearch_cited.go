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
)

// firecrawlBaseURL and firecrawlHTTPClient are shared with firecrawl tool.

type websearchCitedTool struct{}

func (websearchCitedTool) Name() string   { return "websearch_cited" }
func (websearchCitedTool) Mutating() bool { return false }

type Citation struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

func (websearchCitedTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Query      string `json:"query"`
		MaxResults int    `json:"maxResults,omitempty"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.Query) == "" {
		return Result{}, errors.New("websearch_cited: query required")
	}
	if in.MaxResults <= 0 {
		in.MaxResults = 5
	}
	apiKey := strings.TrimSpace(os.Getenv("FIRECRAWL_API_KEY"))
	if apiKey == "" {
		return Result{}, errors.New("websearch_cited: missing FIRECRAWL_API_KEY; run `firecrawl login --browser` or set the env var")
	}
	payload := map[string]any{"query": in.Query, "limit": in.MaxResults}
	bodyBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/v1/search", firecrawlBaseURL), strings.NewReader(string(bodyBytes)))
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
		return Result{}, fmt.Errorf("websearch_cited: HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	var parsed struct {
		Data []Citation `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Result{}, err
	}
	var bld strings.Builder
	for i, c := range parsed.Data {
		title := c.Title
		if title == "" {
			title = c.URL
		}
		fmt.Fprintf(&bld, "%d. [%s](%s)\n   %s\n", i+1, title, c.URL, c.Snippet)
	}
	bld.WriteString("\nCitations:\n")
	for _, c := range parsed.Data {
		bld.WriteString(c.URL)
		bld.WriteByte('\n')
	}
	out, truncated := TruncateOutput([]byte(bld.String()))
	return Result{Output: out, Truncated: truncated}, nil
}

// NewWebsearchCitedTool returns a websearch_cited tool instance.
func NewWebsearchCitedTool() Tool { return websearchCitedTool{} }
