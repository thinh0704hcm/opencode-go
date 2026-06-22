package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFirecrawlTool_Success(t *testing.T) {
	// Set up fake Firecrawl server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/scrape" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"markdown":"# Hello"}}`))
	}))
	defer server.Close()

	// Override globals.
	firecrawlBaseURL = server.URL
	firecrawlHTTPClient = server.Client()
	// Set API key.
	t.Setenv("FIRECRAWL_API_KEY", "testkey")

	tool := NewFirecrawlTool()
	input, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	res, err := tool.Execute(context.Background(), input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Output == "" || !contains(res.Output, "Hello") {
		t.Fatalf("unexpected output: %s", res.Output)
	}
}

func TestFirecrawlTool_MissingKey(t *testing.T) {
	// Ensure env var is not set.
	t.Setenv("FIRECRAWL_API_KEY", "")
	tool := NewFirecrawlTool()
	input, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	_, err := tool.Execute(context.Background(), input, nil)
	if err == nil || !contains(err.Error(), "missing FIRECRAWL_API_KEY") {
		t.Fatalf("expected missing API key error, got %v", err)
	}
}

func contains(s, substr string) bool { return strings.Contains(s, substr) }
