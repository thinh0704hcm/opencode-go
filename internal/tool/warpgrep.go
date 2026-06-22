package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type warpGrepInput struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path,omitempty"`
	MaxMatches int    `json:"maxMatches,omitempty"`
	MaxBytes   int    `json:"maxBytes,omitempty"`
}

type warpGrepTool struct{}

func (warpGrepTool) Name() string   { return "warpgrep" }
func (warpGrepTool) Mutating() bool { return false }

func NewWarpGrepTool() Tool { return warpGrepTool{} }

func (warpGrepTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in warpGrepInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.Pattern) == "" {
		return Result{}, errors.New("warpgrep: pattern required")
	}
	if in.Path == "" {
		in.Path = "."
	}
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return Result{}, fmt.Errorf("warpgrep: invalid pattern: %w", err)
	}
	root, err := sb.ResolveReadOnly(in.Path)
	if err != nil {
		return Result{}, err
	}
	var b strings.Builder
	rootAbs := sb.Root()
	matches := 0
	totalBytes := 0
	stopErr := errors.New("stop")
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// Relative path for output.
		relPath, err := filepath.Rel(rootAbs, p)
		if err != nil {
			return nil
		}
		// Ensure sandbox read‑only access.
		if _, err := sb.ResolveReadOnly(relPath); err != nil {
			return nil
		}
		f, err := sb.OpenReadOnlyNoFollow(relPath)
		if err != nil {
			return nil
		}
		scanner := bufio.NewScanner(f)
		// increase buffer to handle long lines (up to 10 MiB)
		scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
		lineno := 0
		for scanner.Scan() {
			lineno++
			line := scanner.Text()
			if re.MatchString(line) {
				fmt.Fprintf(&b, "%s:%d:%s\n", relPath, lineno, line)
				matches++
				totalBytes += len(line)
				if in.MaxMatches > 0 && matches >= in.MaxMatches {
					f.Close()
					return stopErr
				}
				if in.MaxBytes > 0 && totalBytes >= in.MaxBytes {
					f.Close()
					return stopErr
				}
			}
		}
		// If scanning error (e.g., line too long), skip file gracefully.
		if err := scanner.Err(); err != nil {
			// ignore and continue
		}
		f.Close()
		return nil
	})
	// ignore stop error
	out, truncated := TruncateOutput([]byte(b.String()))
	return Result{Output: out, Truncated: truncated}, nil
}
