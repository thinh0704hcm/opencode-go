package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebsearchCitedTool_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"url":"http://x","title":"T","snippet":"S"}]}`))
	}))
	defer server.Close()

	firecrawlBaseURL = server.URL
	firecrawlHTTPClient = server.Client()
	t.Setenv("FIRECRAWL_API_KEY", "testkey")

	tool := NewWebsearchCitedTool()
	input, _ := json.Marshal(map[string]string{"query": "test"})
	res, err := tool.Execute(context.Background(), input, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Output, "T") || !strings.Contains(res.Output, "http://x") {
		t.Fatalf("unexpected output: %s", res.Output)
	}
}

func TestWebsearchCitedTool_EmptyQuery(t *testing.T) {
	t.Setenv("FIRECRAWL_API_KEY", "testkey")
	tool := NewWebsearchCitedTool()
	input, _ := json.Marshal(map[string]string{"query": ""})
	_, err := tool.Execute(context.Background(), input, nil)
	if err == nil || !strings.Contains(err.Error(), "query required") {
		t.Fatalf("expected query required error, got %v", err)
	}
}
