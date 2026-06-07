package server

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

// findMatch is one text-search hit: a workdir-relative path, 1-based line
// number, and the matching line text.
type findMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// handleFind serves GET /find?pattern=<regex>: a content search rooted at the
// server workdir. Returns a JSON array of {path,line,text}. Skips .git,
// node_modules, hidden dirs, and non-regular/large files. Capped at 200 hits.
func (s *Server) handleFind(w http.ResponseWriter, r *http.Request) {
	pattern := r.URL.Query().Get("pattern")
	if strings.TrimSpace(pattern) == "" {
		writeError(w, http.StatusBadRequest, "pattern query param required")
		return
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pattern: "+err.Error())
		return
	}
	root := s.workdir
	if root == "" {
		root = "."
	}
	const maxHits = 200
	matches := []findMatch{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || (name != "." && strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		f, oerr := os.Open(path)
		if oerr != nil {
			return nil
		}
		defer f.Close()
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineno := 0
		for sc.Scan() {
			lineno++
			line := sc.Text()
			if re.MatchString(line) {
				matches = append(matches, findMatch{Path: rel, Line: lineno, Text: line})
				if len(matches) >= maxHits {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	writeJSON(w, http.StatusOK, matches)
}
