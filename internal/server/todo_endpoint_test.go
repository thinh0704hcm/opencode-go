package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencode-go/opencode-go/internal/provider"
	"github.com/opencode-go/opencode-go/internal/tool"
)

func TestTodoEndpointsEmptyAndAPI(t *testing.T) {
	dir := t.TempDir()
	srv := New(Options{Provider: provider.NewMock(""), Model: "mock", Workdir: dir})
	sess := srv.store.CreateSession("", "test", dir)

	// server for HTTP requests
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// non-API empty GET
	resp, err := http.Get(ts.URL + "/session/" + sess.ID + "/todo")
	if err != nil {
		t.Fatalf("GET todo: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET todo status = %d", resp.StatusCode)
	}
	var got []any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}

	// API empty GET
	resp2, err := http.Get(ts.URL + "/api/session/" + sess.ID + "/todo")
	if err != nil {
		t.Fatalf("GET api todo: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET api todo status = %d", resp2.StatusCode)
	}
	var got2 []any
	if err := json.NewDecoder(resp2.Body).Decode(&got2); err != nil {
		t.Fatalf("decode api: %v", err)
	}
	if len(got2) != 0 {
		t.Fatalf("expected empty slice api, got %v", got2)
	}

	// write todos via tool
	sb, _ := tool.New(dir)
	call := provider.ToolCall{ID: "c1", Name: "todowrite", Input: json.RawMessage(`{"todos":[{"content":"c","status":"s","priority":"p"}]}`)}
	out, isErr := executeToolCall(withSessionID(context.Background(), sess.ID), srv.tools, sb, call)
	if isErr {
		t.Fatalf("todowrite error: %s", out)
	}

	// verify non-API returns todo
	resp3, err := http.Get(ts.URL + "/session/" + sess.ID + "/todo")
	if err != nil {
		t.Fatalf("GET todo after write: %v", err)
	}
	defer resp3.Body.Close()
	var after []struct{ Content, Status, Priority string }
	if err := json.NewDecoder(resp3.Body).Decode(&after); err != nil {
		t.Fatalf("decode after: %v", err)
	}
	if len(after) != 1 || after[0].Content != "c" {
		t.Fatalf("unexpected after todo: %+v", after)
	}

	// verify API returns same
	resp4, err := http.Get(ts.URL + "/api/session/" + sess.ID + "/todo")
	if err != nil {
		t.Fatalf("GET api todo after write: %v", err)
	}
	defer resp4.Body.Close()
	var after2 []struct{ Content, Status, Priority string }
	if err := json.NewDecoder(resp4.Body).Decode(&after2); err != nil {
		t.Fatalf("decode api after: %v", err)
	}
	if len(after2) != 1 || after2[0].Content != "c" {
		t.Fatalf("api after mismatch: %+v", after2)
	}
}
