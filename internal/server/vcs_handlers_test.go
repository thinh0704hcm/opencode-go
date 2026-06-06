package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
