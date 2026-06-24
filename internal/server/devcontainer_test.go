// Blocked: depends on gated devcontainer.go handlers and request/response types.
//go:build opencode_wip

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDevcontainerDisabled(t *testing.T) {
	t.Setenv("DEV_CONTAINER_ENABLED", "")
	s := newTestServer()
	body := `{"sessionID":"x"}`
	req := httptest.NewRequest("POST", "/experimental/devcontainer/bootstrap", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleDevcontainerBootstrap(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	var resp devcontainerResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !strings.Contains(resp.Error, "disabled") {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestDevcontainerMissingImage(t *testing.T) {
	t.Setenv("DEV_CONTAINER_ENABLED", "1")
	t.Setenv("DEV_CONTAINER_IMAGE", "")
	s := newTestServer()
	body := `{"sessionID":"x"}`
	req := httptest.NewRequest("POST", "/experimental/devcontainer/bootstrap", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleDevcontainerBootstrap(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp devcontainerResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !strings.Contains(resp.Error, "DEV_CONTAINER_IMAGE") {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestDevcontainerSuccess(t *testing.T) {
	t.Setenv("DEV_CONTAINER_ENABLED", "1")
	t.Setenv("DEV_CONTAINER_IMAGE", "alpine")
	// stub runner
	called := false
	var gotWorkdir string
	var gotCmd []string
	var gotImage string
	orig := devcontainerRunner
	devcontainerRunner = func(ctx context.Context, cfg devcontainerConfig, workdir string, cmd []string) (string, error) {
		called = true
		gotWorkdir = workdir
		gotCmd = cmd
		gotImage = cfg.Image
		return "ok-output", nil
	}
	defer func() { devcontainerRunner = orig }()
	s := newTestServer()
	body := `{"sessionID":"s1","cmd":["echo","hi"]}`
	req := httptest.NewRequest("POST", "/experimental/devcontainer/bootstrap", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleDevcontainerBootstrap(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !called {
		t.Fatalf("runner not called")
	}
	if gotCmd[0] != "echo" || gotCmd[1] != "hi" {
		t.Fatalf("unexpected cmd %v", gotCmd)
	}
	if gotWorkdir == "" {
		t.Fatalf("workdir empty")
	}
	var resp devcontainerResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Output != "ok-output" {
		t.Fatalf("unexpected output %s", resp.Output)
	}
	if gotImage != "alpine" {
		t.Fatalf("unexpected image %s", gotImage)
	}

}

func TestDevcontainerRunnerError(t *testing.T) {
	t.Setenv("DEV_CONTAINER_ENABLED", "1")
	t.Setenv("DEV_CONTAINER_IMAGE", "alpine")
	orig := devcontainerRunner
	devcontainerRunner = func(ctx context.Context, cfg devcontainerConfig, workdir string, cmd []string) (string, error) {
		return "partial", errors.New("boom")
	}
	defer func() { devcontainerRunner = orig }()
	s := newTestServer()
	body := `{"sessionID":"s1","cmd":["foo"]}`
	req := httptest.NewRequest("POST", "/experimental/devcontainer/bootstrap", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleDevcontainerBootstrap(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	var resp devcontainerResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !strings.Contains(resp.Error, "boom") {
		t.Fatalf("unexpected error %v", resp.Error)
	}
	if resp.Output != "partial" {
		t.Fatalf("unexpected output %s", resp.Output)
	}
}
