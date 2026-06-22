package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type fastApplyOp struct {
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Content string `json:"content"`
	Old     string `json:"old,omitempty"`
}

type fastApplyInput struct {
	Operations []fastApplyOp `json:"operations"`
}

type fastApplyTool struct{}

func (fastApplyTool) Name() string   { return "fastapply" }
func (fastApplyTool) Mutating() bool { return true }

func NewFastApplyTool() Tool { return fastApplyTool{} }

func (fastApplyTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in fastApplyInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if len(in.Operations) == 0 {
		return Result{}, errors.New("fastapply: no operations provided")
	}
	// Stage changes.
	staged := make(map[string][]byte)
	for i, op := range in.Operations {
		abs, err := sb.Resolve(op.Path)
		if err != nil {
			return Result{}, fmt.Errorf("op %d: %w", i, err)
		}
		switch op.Mode {
		case "write":
			staged[abs] = []byte(op.Content)
		case "edit":
			cur, err := os.ReadFile(abs)
			if err != nil {
				return Result{}, fmt.Errorf("op %d: read file: %w", i, err)
			}
			if op.Old == "" {
				return Result{}, fmt.Errorf("op %d: old string required for edit", i)
			}
			count := strings.Count(string(cur), op.Old)
			if count != 1 {
				return Result{}, fmt.Errorf("op %d: old string not unique", i)
			}
			newContent := strings.Replace(string(cur), op.Old, op.Content, 1)
			staged[abs] = []byte(newContent)
		default:
			return Result{}, fmt.Errorf("op %d: unknown mode %q", i, op.Mode)
		}
	}
	// Commit atomically.
	// Phase A: write all temp files.
	tmpMap := make(map[string]string) // tmpPath -> finalPath
	for path, data := range staged {
		dir := filepath.Dir(path)
		tmp, err := os.CreateTemp(dir, "tmp_fastapply_*")
		if err != nil {
			// cleanup any temps created so far
			for t := range tmpMap {
				os.Remove(t)
			}
			return Result{}, fmt.Errorf("write temp for %s: %w", path, err)
		}
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			for t := range tmpMap {
				os.Remove(t)
			}
			return Result{}, fmt.Errorf("write temp for %s: %w", path, err)
		}
		tmp.Close()
		tmpMap[tmp.Name()] = path
	}
	// Phase B: rename all temp files to final locations.
	for tmpPath, finalPath := range tmpMap {
		if err := os.Rename(tmpPath, finalPath); err != nil {
			// best‑effort: report error; cannot fully rollback renamed files
			return Result{}, fmt.Errorf("rename temp for %s: %w", finalPath, err)
		}
	}
	// Emit WakaTime heartbeat for each changed file.
	for _, op := range in.Operations {
		asyncWakaHeartbeat(op.Path)
	}
	var sbld strings.Builder
	for i, op := range in.Operations {
		fmt.Fprintf(&sbld, "%s %s", op.Mode, op.Path)
		if i < len(in.Operations)-1 {
			sbld.WriteByte('\n')
		}
	}
	out, truncated := TruncateOutput([]byte(sbld.String()))
	return Result{Output: out, Truncated: truncated}, nil
}
