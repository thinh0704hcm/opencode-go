package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleVCSNonGitDir asserts GET /vcs against a non-git temp dir returns
// 200 with a null branch.
func TestHandleVCSNonGitDir(t *testing.T) {
	dir := t.TempDir()

	srv := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/vcs?directory="+dir, nil)
	rec := httptest.NewRecorder()

	srv.handleVCS(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp vcsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v (body=%q)", err, rec.Body.String())
	}

	if resp.Branch != nil {
		t.Fatalf("branch = %v, want nil", *resp.Branch)
	}
}

// TestHandleVCSApplyInvalidBody ensures POST /vcs/apply with malformed JSON returns error.
func TestHandleVCSApplyInvalidBody(t *testing.T) {
	srv := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/vcs/apply?directory=/tmp", strings.NewReader("{invalid}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleVCSApply(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp vcsApplyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Applied {
		t.Fatalf("applied true, want false")
	}
	if resp.Error == "" {
		t.Fatalf("expected error message")
	}
}

// TestHandleVCSApplySuccess ensures applying an empty diff succeeds.
func TestHandleVCSApplySuccess(t *testing.T) {
	srv := &Server{}
	// empty diff should succeed (git apply with empty input returns ok)
	body := `{"diff":""}`
	req := httptest.NewRequest(http.MethodPost, "/vcs/apply?directory=/tmp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.handleVCSApply(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp vcsApplyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Applied {
		t.Fatalf("applied false, want true")
	}
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
}
