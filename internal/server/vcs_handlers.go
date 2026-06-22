package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/opencode-go/opencode-go/internal/tool"
)

// vcsCmdTimeout bounds each git invocation so a hung repo cannot stall a request.
const vcsCmdTimeout = 10 * time.Second

// vcsDiffCap is the inline fallback cap (~256KB) when tool.TruncateOutput is
// unavailable; TruncateOutput is preferred when present.
const vcsDiffCap = 256 * 1024

// runGit executes `git args...` with cmd.Dir = dir under a short timeout.
// It returns trimmed stdout, raw stderr, and any exec/exit error.
func runGit(dir string, stdin []byte, args ...string) (stdout string, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), vcsCmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// vcsStatusEntry is one porcelain row: a 2-char status code and the path.
type vcsStatusEntry struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// vcsDiffResponse is the GET /vcs/diff body.
type vcsDiffResponse struct {
	Diff string `json:"diff"`
}

// vcsApplyRequest is the POST /vcs/apply body.
type vcsApplyRequest struct {
	Diff string `json:"diff"`
}

// vcsApplyResponse is the POST /vcs/apply body.
type vcsApplyResponse struct {
	Applied bool   `json:"applied"`
	Error   string `json:"error,omitempty"`
}

// handleVCS serves GET /vcs -> {branch, default_branch}. Returns null fields
// gracefully (200) when the directory is not a git repository.
func (s *Server) handleVCS(w http.ResponseWriter, r *http.Request) {
	dir := directoryParam(r)

	resp := vcsResponse{Branch: nil, DefaultBranch: nil}

	if out, _, err := runGit(dir, nil, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		if b := strings.TrimSpace(out); b != "" {
			resp.Branch = &b
		}
	}

	if def := detectDefaultBranch(dir); def != "" {
		resp.DefaultBranch = &def
	}

	writeJSON(w, http.StatusOK, resp)
}

// detectDefaultBranch resolves origin/HEAD, then falls back to local main/master
// detection. Returns "" when none can be determined (or not a repo).
func detectDefaultBranch(dir string) string {
	if out, _, err := runGit(dir, nil, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		ref := strings.TrimSpace(out)
		ref = strings.TrimPrefix(ref, "origin/")
		if ref != "" {
			return ref
		}
	}

	for _, cand := range []string{"main", "master"} {
		if _, _, err := runGit(dir, nil, "rev-parse", "--verify", "refs/heads/"+cand); err == nil {
			return cand
		}
	}

	return ""
}

// handleVCSStatus serves GET /vcs/status -> [{path,status}]. Empty array when
// clean or not a repo.
func (s *Server) handleVCSStatus(w http.ResponseWriter, r *http.Request) {
	dir := directoryParam(r)

	entries := []vcsStatusEntry{}

	out, _, err := runGit(dir, nil, "status", "--porcelain")
	if err == nil {
		sc := bufio.NewScanner(strings.NewReader(out))
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if len(line) < 3 {
				continue
			}
			code := line[:2]
			path := strings.TrimSpace(line[3:])
			entries = append(entries, vcsStatusEntry{Path: path, Status: code})
		}
	}

	writeJSON(w, http.StatusOK, entries)
}

// gitDiff runs `git diff` and returns the (possibly truncated) working-tree
// diff. Returns "" when not a repo.
func gitDiff(dir string) string {
	out, _, err := runGit(dir, nil, "diff")
	if err != nil {
		return ""
	}

	capped, _ := tool.TruncateOutput([]byte(out))
	if len(capped) > vcsDiffCap {
		capped = capped[:vcsDiffCap]
	}
	return capped
}

// handleVCSDiff serves GET /vcs/diff -> {diff}.
func (s *Server) handleVCSDiff(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, vcsDiffResponse{Diff: gitDiff(directoryParam(r))})
}

// handleVCSDiffRaw serves GET /vcs/diff/raw -> raw text/plain `git diff`.
func (s *Server) handleVCSDiffRaw(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, gitDiff(directoryParam(r)))
}

// handleVCSApply serves POST /vcs/apply -> {applied, error?}. Feeds the request
// diff to `git apply` on stdin, constrained to cmd.Dir. Failures return
// {applied:false,error:<stderr>} with HTTP 200.
func (s *Server) handleVCSApply(w http.ResponseWriter, r *http.Request) {
    // Require JSON content type
    if !requireJSON(w, r) {
        return
    }
    dir := directoryParam(r)

    raw, err := io.ReadAll(r.Body)
    if err != nil {
        writeJSON(w, http.StatusInternalServerError, vcsApplyResponse{Applied: false, Error: err.Error()})
        return
    }
    if len(bytes.TrimSpace(raw)) == 0 {
        writeJSON(w, http.StatusBadRequest, vcsApplyResponse{Applied: false, Error: "empty request body"})
        return
    }

    var req vcsApplyRequest
    if err := json.Unmarshal(raw, &req); err != nil {
        writeJSON(w, http.StatusOK, vcsApplyResponse{Applied: false, Error: "invalid request body"})
        return
    }

    if strings.TrimSpace(req.Diff) == "" {
        writeJSON(w, http.StatusOK, vcsApplyResponse{Applied: true})
        return
    }

    _, stderr, err := runGit(dir, []byte(req.Diff), "apply")
    if err != nil {
        msg := strings.TrimSpace(stderr)
        if msg == "" {
            msg = err.Error()
        }
        writeJSON(w, http.StatusOK, vcsApplyResponse{Applied: false, Error: msg})
        return
    }

    writeJSON(w, http.StatusOK, vcsApplyResponse{Applied: true})
}

type vcsDiffStatEntry struct {
	File      string `json:"file"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

func gitDiffStat(dir string) ([]vcsDiffStatEntry, error) {
	out, _, err := runGit(dir, nil, "diff", "--numstat", "HEAD")
	if err != nil {
		return nil, err
	}
	var entries []vcsDiffStatEntry
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		adds, _ := strconv.Atoi(fields[0])
		dels, _ := strconv.Atoi(fields[1])
		entries = append(entries, vcsDiffStatEntry{
			File:      fields[2],
			Additions: adds,
			Deletions: dels,
		})
	}
	return entries, nil
}
