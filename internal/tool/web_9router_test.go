package tool

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSearchTool(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"provider":"brave-search","query":"q","answer":"42","results":[{"title":"T1","url":"https://a","snippet":"s1"},{"title":"T2","url":"https://b","snippet":""}]}`)
	}))
	defer ts.Close()

	tool := NewWebSearchTool(ts.URL+"/v1", "sk-key", ts.Client())
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"q"}`), nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotPath != "/v1/search" {
		t.Errorf("path = %q, want /v1/search", gotPath)
	}
	if gotAuth != "Bearer sk-key" {
		t.Errorf("auth = %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"model":"search-combo"`) {
		t.Errorf("body missing default model: %s", gotBody)
	}
	for _, want := range []string{"Answer: 42", "T1", "https://a", "s1", "T2", "https://b"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output missing %q:\n%s", want, res.Output)
		}
	}
}

func TestWebSearchToolRequiresQuery(t *testing.T) {
	tool := NewWebSearchTool("http://x/v1", "", nil)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`), nil); err == nil {
		t.Fatal("expected error for missing query")
	}
}

func TestWebFetch9RouterTool(t *testing.T) {
	var gotPath, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, `{"provider":"jina-reader","url":"https://x","title":"Hello","content":{"format":"markdown","text":"# body","length":6}}`)
	}))
	defer ts.Close()

	tool := NewWebFetch9RouterTool(ts.URL+"/v1", "", ts.Client())
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"url":"https://x"}`), nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotPath != "/v1/web/fetch" {
		t.Errorf("path = %q, want /v1/web/fetch", gotPath)
	}
	if !strings.Contains(gotBody, `"model":"fetch-combo"`) || !strings.Contains(gotBody, `"format":"markdown"`) {
		t.Errorf("body wrong defaults: %s", gotBody)
	}
	if !strings.Contains(res.Output, "# Hello") || !strings.Contains(res.Output, "# body") {
		t.Errorf("output missing title/content:\n%s", res.Output)
	}
}

func TestWebFetch9RouterEmptyContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"content":{"text":"","length":0}}`)
	}))
	defer ts.Close()
	tool := NewWebFetch9RouterTool(ts.URL+"/v1", "", ts.Client())
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"url":"https://x"}`), nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Output, "no extractable content") {
		t.Errorf("want graceful empty message, got %q", res.Output)
	}
}
