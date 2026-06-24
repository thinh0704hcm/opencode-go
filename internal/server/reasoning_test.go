package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/opencode-go/opencode-go/internal/provider"
)

type reasoningRecordingProvider struct {
	mu       sync.Mutex
	requests []provider.ChatRequest
}

func (p *reasoningRecordingProvider) ID() string { return "test" }

func (p *reasoningRecordingProvider) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatChunk, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()

	ch := make(chan provider.ChatChunk, 2)
	ch <- provider.ChatChunk{TextDelta: "ok"}
	ch <- provider.ChatChunk{FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func (p *reasoningRecordingProvider) snapshot() []provider.ChatRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]provider.ChatRequest, len(p.requests))
	copy(out, p.requests)
	return out
}

func hasString(list any, want string) bool {
	switch v := list.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == want {
				return true
			}
		}
	case []string:
		for _, s := range v {
			if s == want {
				return true
			}
		}
	}
	return false
}

func postPrompt(t *testing.T, baseURL, sessionID, variant string) {
	t.Helper()
	body := []byte(`{"model":{"providerID":"test","modelID":"mock","variant":"` + variant + `"},"agent":"build","parts":[{"type":"text","text":"hi"}]}`)
	resp, err := http.Post(baseURL+"/session/"+sessionID+"/message", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST variant %s status = %d, want 200", variant, resp.StatusCode)
	}
}

func TestReasoningEffortVariantObjectPlumbing(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	configJSON := []byte(`{
		"provider": {
			"test": {
				"name": "test",
				"models": {
					"mock": {
						"id": "mock",
						"name": "Mock",
						"variants": {
							"xhigh": {"reasoningEffort": "xhigh", "include": ["reasoning.encrypted_content"]},
							"custom": {"include": ["custom.include"]}
						}
					}
				}
			}
		}
	}`)
	if err := os.WriteFile(filepath.Join(configDir, "opencode.json"), configJSON, 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &reasoningRecordingProvider{}
	srv := New(Options{Provider: rec, ConfiguredProviderID: "test", Model: "mock", Workdir: tmpDir})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	sid1 := "reasoning-xhigh"
	srv.store.CreateSessionWithID(sid1, "", "proj", tmpDir)
	postPrompt(t, ts.URL, sid1, "xhigh")

	reqs := rec.snapshot()
	if len(reqs) < 1 {
		t.Fatalf("provider received %d requests, want at least 1", len(reqs))
	}
	if got := reqs[0].ReasoningEffort; got != "xhigh" {
		t.Fatalf("ReasoningEffort = %q, want xhigh", got)
	}
	if !hasString(reqs[0].ExtraBody["include"], "reasoning.encrypted_content") {
		t.Fatalf("ExtraBody include = %#v, want reasoning.encrypted_content", reqs[0].ExtraBody["include"])
	}

	sid2 := "reasoning-custom"
	srv.store.CreateSessionWithID(sid2, "", "proj", tmpDir)
	postPrompt(t, ts.URL, sid2, "custom")

	reqs = rec.snapshot()
	if len(reqs) < 2 {
		t.Fatalf("provider received %d requests, want at least 2", len(reqs))
	}
	if got := reqs[1].ReasoningEffort; got != "" {
		t.Fatalf("custom variant ReasoningEffort = %q, want empty", got)
	}
	if !hasString(reqs[1].ExtraBody["include"], "custom.include") {
		t.Fatalf("custom ExtraBody include = %#v, want custom.include", reqs[1].ExtraBody["include"])
	}
}
