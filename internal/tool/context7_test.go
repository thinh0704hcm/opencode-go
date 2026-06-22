package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestContext7RemoteBaseURLValidation(t *testing.T) {
	os.Setenv("CONTEXT7_BASE_URL", "://bad")
	defer os.Unsetenv("CONTEXT7_BASE_URL")
	_, err := context7Tool{}.Execute(context.Background(), []byte(`{"package":"p","query":"q","remote":true}`), nil)
	if err == nil {
		t.Fatalf("expected error for invalid CONTEXT7_BASE_URL")
	}
}

func TestContext7MCPMissingEnv(t *testing.T) {
	os.Unsetenv("CONTEXT7_MCP_URL")
	_, err := context7Tool{}.Execute(context.Background(), []byte(`{"package":"p","query":"q","mode":"mcp"}`), nil)
	if err == nil || !strings.Contains(err.Error(), "context7 mcp not configured") {
		t.Fatalf("expected missing config error, got %v", err)
	}
}

func TestContext7MCPInvalidURL(t *testing.T) {
	os.Setenv("CONTEXT7_MCP_URL", "ftp://example.com")
	defer os.Unsetenv("CONTEXT7_MCP_URL")
	_, err := context7Tool{}.Execute(context.Background(), []byte(`{"package":"p","query":"q","mode":"mcp"}`), nil)
	if err == nil || !strings.Contains(err.Error(), "invalid CONTEXT7_MCP_URL") {
		t.Fatalf("expected invalid URL error, got %v", err)
	}
}

func TestContext7MCPAuthAndResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer testtoken" {
			t.Fatalf("expected Authorization Bearer testtoken, got %s", got)
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		params, _ := req["params"].(map[string]any)
		if name, _ := params["name"].(string); name != "custom-tool" {
			t.Fatalf("expected tool name custom-tool, got %v", name)
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"jsonrpc": "2.0", "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "hello world"}}, "isError": false}, "id": 1}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	os.Setenv("CONTEXT7_MCP_URL", srv.URL)
	defer os.Unsetenv("CONTEXT7_MCP_URL")
	os.Setenv("CONTEXT7_MCP_AUTH", "testtoken")
	defer os.Unsetenv("CONTEXT7_MCP_AUTH")
	out, err := context7Tool{}.Execute(context.Background(), []byte(`{"package":"p","query":"q","mode":"mcp","tool":"custom-tool"}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var outMap map[string]any
	json.Unmarshal([]byte(out.Output), &outMap)
	if outMap["mode"] != "mcp" || outMap["tool"] != "custom-tool" || outMap["content"] != "hello world" {
		t.Fatalf("unexpected output: %v", outMap)
	}
}

func TestContext7MCPNon2xxNoLeak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauth"}`))
	}))
	defer srv.Close()
	os.Setenv("CONTEXT7_MCP_URL", srv.URL)
	defer os.Unsetenv("CONTEXT7_MCP_URL")
	os.Setenv("CONTEXT7_MCP_AUTH", "secret")
	defer os.Unsetenv("CONTEXT7_MCP_AUTH")
	_, err := context7Tool{}.Execute(context.Background(), []byte(`{"package":"p","query":"q","mode":"mcp"}`), nil)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected error without token leak, got %v", err)
	}
}

func TestContext7MCPTimeoutClamping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","result":{"content":"ok"},"id":1}`))
	}))
	defer srv.Close()
	os.Setenv("CONTEXT7_MCP_URL", srv.URL)
	defer os.Unsetenv("CONTEXT7_MCP_URL")
	out, err := context7Tool{}.Execute(context.Background(), []byte(`{"package":"p","query":"q","mode":"mcp"}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var outMap map[string]any
	json.Unmarshal([]byte(out.Output), &outMap)
	if outMap["content"] != "ok" {
		t.Fatalf("unexpected content: %v", outMap["content"])
	}
}

func TestContext7MCPInvalidJSONRPC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"result":{"content":"x"},"id":1}`))
	}))
	defer srv.Close()
	os.Setenv("CONTEXT7_MCP_URL", srv.URL)
	defer os.Unsetenv("CONTEXT7_MCP_URL")
	_, err := context7Tool{}.Execute(context.Background(), []byte(`{"package":"p","query":"q","mode":"mcp"}`), nil)
	if err == nil || !strings.Contains(err.Error(), "invalid MCP jsonrpc version") {
		t.Fatalf("expected version error, got %v", err)
	}
}

func TestContext7MCPTopLevelError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"boom"},"id":1}`))
	}))
	defer srv.Close()
	os.Setenv("CONTEXT7_MCP_URL", srv.URL)
	defer os.Unsetenv("CONTEXT7_MCP_URL")
	os.Setenv("CONTEXT7_MCP_AUTH", "secret-token")
	defer os.Unsetenv("CONTEXT7_MCP_AUTH")
	_, err := context7Tool{}.Execute(context.Background(), []byte(`{"package":"p","query":"q","mode":"mcp"}`), nil)
	if err == nil || !strings.Contains(err.Error(), "MCP error -32000: boom") || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("expected sanitized MCP error, got %v", err)
	}
}

func TestContext7MCPInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{bad`))
	}))
	defer srv.Close()
	os.Setenv("CONTEXT7_MCP_URL", srv.URL)
	defer os.Unsetenv("CONTEXT7_MCP_URL")
	_, err := context7Tool{}.Execute(context.Background(), []byte(`{"package":"p","query":"q","mode":"mcp"}`), nil)
	if err == nil || !strings.Contains(err.Error(), "invalid MCP JSON response") {
		t.Fatalf("expected json error, got %v", err)
	}
}
