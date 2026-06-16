package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type planSaveTool struct{}
type planReadTool struct{}

func (planSaveTool) Name() string   { return "plan_save" }
func (planSaveTool) Mutating() bool { return true }

func (planReadTool) Name() string   { return "plan_read" }
func (planReadTool) Mutating() bool { return false }

func (planSaveTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	path, err := workspacePlanPath(sb)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(path, []byte(in.Content), 0o644); err != nil {
		return Result{}, err
	}
	return Result{Output: "saved " + path}, nil
}

func (planReadTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	path, err := workspacePlanPath(sb)
	if err != nil {
		return Result{}, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Result{Output: "no plan saved"}, nil
	}
	if err != nil {
		return Result{}, err
	}
	out, truncated := TruncateOutput(b)
	return Result{Output: out, Truncated: truncated}, nil
}

func workspacePlanPath(sb *Sandbox) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(sb.Root()))
	projectID := filepath.Base(sb.Root()) + "-" + hex.EncodeToString(sum[:])[:12]
	return filepath.Join(home, ".local", "share", "opencode", "workspace", projectID, "workspace", "plan.md"), nil
}

type worktreeCreateTool struct{}
type worktreeDeleteTool struct{}

func (worktreeCreateTool) Name() string   { return "worktree_create" }
func (worktreeCreateTool) Mutating() bool { return true }

func (worktreeDeleteTool) Name() string   { return "worktree_delete" }
func (worktreeDeleteTool) Mutating() bool { return true }

var branchNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)

func (worktreeCreateTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Branch     string `json:"branch"`
		BaseBranch string `json:"baseBranch"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if !validBranchName(in.Branch) {
		return Result{}, fmt.Errorf("invalid branch name: %q", in.Branch)
	}
	repo, err := gitRepoName(ctx, sb.Root())
	if err != nil {
		return Result{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{}, err
	}
	path := filepath.Join(home, ".local", "share", "opencode", "worktrees", repo, safeBranchPath(in.Branch))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, err
	}
	args := []string{"worktree", "add", "-b", in.Branch, path}
	if in.BaseBranch != "" {
		if !validBranchName(in.BaseBranch) {
			return Result{}, fmt.Errorf("invalid base branch name: %q", in.BaseBranch)
		}
		args = append(args, in.BaseBranch)
	}
	out, err := runGit(ctx, sb.Root(), args...)
	if err != nil {
		return Result{Output: out}, err
	}
	return Result{Output: strings.TrimSpace(out) + "\n" + path}, nil
}

func (worktreeDeleteTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Path  string `json:"path"`
		Force bool   `json:"force"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if in.Path == "" {
		return Result{}, fmt.Errorf("worktree_delete requires path; session-to-worktree mapping is not implemented")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{}, err
	}
	base := filepath.Join(home, ".local", "share", "opencode", "worktrees")
	abs, err := filepath.Abs(in.Path)
	if err != nil {
		return Result{}, err
	}
	if !strings.HasPrefix(abs, base+string(os.PathSeparator)) {
		return Result{}, fmt.Errorf("refusing to delete outside %s", base)
	}
	args := []string{"worktree", "remove"}
	if in.Force {
		args = append(args, "--force")
	}
	args = append(args, abs)
	out, err := runGit(ctx, sb.Root(), args...)
	if err != nil {
		return Result{Output: out}, err
	}
	return Result{Output: strings.TrimSpace(out)}, nil
}

func validBranchName(name string) bool {
	if !branchNameRe.MatchString(name) || strings.Contains(name, "..") || strings.Contains(name, "//") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, "/") || strings.Contains(name, "@{") {
		return false
	}
	for _, r := range name {
		if strings.ContainsRune(" ~^:?*[\\", r) || r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func safeBranchPath(branch string) string { return strings.ReplaceAll(branch, "/", "__") }

func gitRepoName(ctx context.Context, dir string) (string, error) {
	out, err := runGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.Base(strings.TrimSpace(out)), nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	out, truncated := TruncateOutput(b)
	if truncated {
		out += "\n[git output truncated]"
	}
	return out, err
}
