package server

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/opencode-go/opencode-go/internal/tool"
)

// handleFindFile serves GET /find/file?query=<q>: a fuzzy filename search rooted
// at the server workdir. Returns a JSON array of workdir-relative paths
// (case-insensitive substring match; empty query lists files). Skips .git,
// node_modules, and hidden directories. Capped at 100 results.
func (s *Server) handleFindFile(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
	root := s.workdir
	if root == "" {
		root = "."
	}
	const maxResults = 100
	var matches []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || (name != "." && strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		if query == "" || strings.Contains(strings.ToLower(rel), query) {
			matches = append(matches, rel)
			if len(matches) >= maxResults {
				return filepath.SkipAll
			}
		}
		return nil
	})
	sort.Strings(matches)
	if matches == nil {
		matches = []string{}
	}
	writeJSON(w, http.StatusOK, matches)
}

// fileContentResponse is the GET /file body.
type fileContentResponse struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// handleFileRead serves GET /file?path=<rel>: returns the contents of a
// workdir-relative file. Path safety is enforced by the sandbox (no absolute
// paths, traversal, or symlink escape).
func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimSpace(r.URL.Query().Get("path"))
	if rel == "" {
		writeError(w, http.StatusBadRequest, "path query param required")
		return
	}
	sb, err := tool.New(s.workdir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sandbox unavailable")
		return
	}
	f, err := sb.OpenFileNoFollow(rel, os.O_RDONLY, 0)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found or not accessible")
		return
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read failed")
		return
	}
	writeJSON(w, http.StatusOK, fileContentResponse{Type: "raw", Content: string(data)})
}
