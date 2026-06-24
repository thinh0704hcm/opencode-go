package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 9Router-backed web tools. Both hit the same OpenAI-compatible gateway the chat
// provider uses (NINEROUTER_URL/.../v1), so one key serves search + fetch with
// provider auto-fallback via the "search-combo" / "fetch-combo" models.

// nineRouterWeb holds shared config for the web tools. base ends in "/v1".
type nineRouterWeb struct {
	base   string // e.g. http://localhost:20128/v1
	apiKey string
	http   *http.Client
}

func (w nineRouterWeb) post(ctx context.Context, path string, body any) ([]byte, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.base+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+w.apiKey)
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("9router %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// --- web search ---------------------------------------------------------------

// WebSearchTool searches the web via 9Router POST /v1/search.
type WebSearchTool struct{ cfg nineRouterWeb }

// NewWebSearchTool builds a search tool. base must end in "/v1".
func NewWebSearchTool(base, apiKey string, client *http.Client) WebSearchTool {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return WebSearchTool{cfg: nineRouterWeb{base: strings.TrimRight(base, "/"), apiKey: apiKey, http: client}}
}

func (WebSearchTool) Name() string   { return "websearch" }
func (WebSearchTool) Mutating() bool { return false }

func (t WebSearchTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var args struct {
		Query      string `json:"query"`
		Model      string `json:"model"`
		Provider   string `json:"provider"`
		MaxResults int    `json:"max_results"`
		SearchType string `json:"search_type"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(args.Query) == "" {
		return Result{}, fmt.Errorf("query is required")
	}
	model := firstNonEmpty(args.Model, args.Provider, "search-combo")
	if args.MaxResults <= 0 {
		args.MaxResults = 5
	}
	reqBody := map[string]any{"model": model, "query": args.Query, "max_results": args.MaxResults}
	if args.SearchType != "" {
		reqBody["search_type"] = args.SearchType
	}
	data, err := t.cfg.post(ctx, "/search", reqBody)
	if err != nil {
		return Result{}, err
	}
	var out struct {
		Provider string `json:"provider"`
		Answer   string `json:"answer"`
		Results  []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return Result{}, fmt.Errorf("decode search response: %w", err)
	}
	var b strings.Builder
	if out.Answer != "" {
		fmt.Fprintf(&b, "Answer: %s\n\n", out.Answer)
	}
	if len(out.Results) == 0 {
		b.WriteString("No results.")
	}
	for i, r := range out.Results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.Title, r.URL)
		if s := strings.TrimSpace(r.Snippet); s != "" {
			fmt.Fprintf(&b, "   %s\n", s)
		}
	}
	return Result{Output: strings.TrimRight(b.String(), "\n")}, nil
}

// --- web fetch ----------------------------------------------------------------

// WebFetch9RouterTool fetches a URL → markdown via 9Router POST /v1/web/fetch.
// It replaces the naive http.GET webfetch when a gateway is configured.
type WebFetch9RouterTool struct{ cfg nineRouterWeb }

// NewWebFetch9RouterTool builds a fetch tool. base must end in "/v1".
func NewWebFetch9RouterTool(base, apiKey string, client *http.Client) WebFetch9RouterTool {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return WebFetch9RouterTool{cfg: nineRouterWeb{base: strings.TrimRight(base, "/"), apiKey: apiKey, http: client}}
}

func (WebFetch9RouterTool) Name() string   { return "webfetch" }
func (WebFetch9RouterTool) Mutating() bool { return false }

func (t WebFetch9RouterTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var args struct {
		URL           string `json:"url"`
		Model         string `json:"model"`
		Provider      string `json:"provider"`
		Format        string `json:"format"`
		MaxCharacters int    `json:"max_characters"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(args.URL) == "" {
		return Result{}, fmt.Errorf("url is required")
	}
	model := firstNonEmpty(args.Model, args.Provider, "fetch-combo")
	format := firstNonEmpty(args.Format, "markdown")
	reqBody := map[string]any{"model": model, "url": args.URL, "format": format}
	if args.MaxCharacters > 0 {
		reqBody["max_characters"] = args.MaxCharacters
	}
	data, err := t.cfg.post(ctx, "/web/fetch", reqBody)
	if err != nil {
		return Result{}, err
	}
	var out struct {
		Title   string `json:"title"`
		Content struct {
			Text   string `json:"text"`
			Length int    `json:"length"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return Result{}, fmt.Errorf("decode fetch response: %w", err)
	}
	text := out.Content.Text
	if strings.TrimSpace(text) == "" {
		return Result{Output: fmt.Sprintf("(no extractable content from %s)", args.URL)}, nil
	}
	if out.Title != "" {
		text = "# " + out.Title + "\n\n" + text
	}
	return Result{Output: text}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
