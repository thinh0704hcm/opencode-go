package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newFindTestServer(t *testing.T, workdir string) *Server {
	t.Helper()
	return &Server{workdir: workdir}
}

func TestHandleFindFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "alpha.go"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "beta.txt"), []byte("y"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "ignored.go"), []byte("z"), 0o644)

	s := newFindTestServer(t, dir)
	req := httptest.NewRequest(http.MethodGet, "/find/file?query=beta", nil)
	rr := httptest.NewRecorder()
	s.handleFindFile(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	var got []string
	json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got) != 1 || !strings.HasSuffix(got[0], "beta.txt") {
		t.Fatalf("want [sub/beta.txt], got %v", got)
	}
	// .git file must be excluded for empty query.
	req2 := httptest.NewRequest(http.MethodGet, "/find/file?query=", nil)
	rr2 := httptest.NewRecorder()
	s.handleFindFile(rr2, req2)
	var all []string
	json.Unmarshal(rr2.Body.Bytes(), &all)
	for _, p := range all {
		if strings.Contains(p, ".git") {
			t.Fatalf(".git should be skipped, got %v", all)
		}
	}
}

func TestHandleFileRead(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("world"), 0o644)
	s := newFindTestServer(t, dir)

	req := httptest.NewRequest(http.MethodGet, "/file?path=hello.txt", nil)
	rr := httptest.NewRecorder()
	s.handleFileRead(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var resp fileContentResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Content != "world" {
		t.Fatalf("content = %q, want world", resp.Content)
	}
	// Traversal/absolute must be rejected (not found / error).
	req2 := httptest.NewRequest(http.MethodGet, "/file?path=../etc/passwd", nil)
	rr2 := httptest.NewRecorder()
	s.handleFileRead(rr2, req2)
	if rr2.Code == 200 {
		t.Fatalf("traversal should not return 200")
	}
}

func TestHandleFind(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\nNEEDLE here\nbye"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("nothing"), 0o644)
	s := newFindTestServer(t, dir)

	req := httptest.NewRequest(http.MethodGet, "/find?pattern=NEEDLE", nil)
	rr := httptest.NewRecorder()
	s.handleFind(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	var got []findMatch
	json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got) != 1 || got[0].Line != 2 || !strings.Contains(got[0].Text, "NEEDLE") {
		t.Fatalf("want one hit at line 2, got %v", got)
	}
	// Missing pattern -> 400.
	rr2 := httptest.NewRecorder()
	s.handleFind(rr2, httptest.NewRequest(http.MethodGet, "/find", nil))
	if rr2.Code != 400 {
		t.Fatalf("missing pattern want 400 got %d", rr2.Code)
	}
}
