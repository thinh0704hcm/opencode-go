package tool

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// constErr is a string-typed error usable without the errors or fmt packages.
type constErr string

func (e constErr) Error() string { return string(e) }

// errOldNotFound is returned by editTool when the target text is absent.
const errOldNotFound = constErr("old string not found")

// writeTool creates or overwrites a file with the provided content.
type writeTool struct{}

func (writeTool) Name() string   { return "write" }
func (writeTool) Mutating() bool { return true }

func (writeTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	abs, err := sb.Resolve(in.Path)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(abs, []byte(in.Content), 0o644); err != nil {
		return Result{}, err
	}
	return Result{Output: "wrote " + in.Path}, nil
}

// editTool replaces the first occurrence of old with new in a file.
type editTool struct{}

func (editTool) Name() string   { return "edit" }
func (editTool) Mutating() bool { return true }

func (editTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Path string `json:"path"`
		Old  string `json:"old"`
		New  string `json:"new"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	abs, err := sb.Resolve(in.Path)
	if err != nil {
		return Result{}, err
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return Result{}, err
	}
	if !strings.Contains(string(content), in.Old) {
		return Result{}, errOldNotFound
	}
	updated := strings.Replace(string(content), in.Old, in.New, 1)
	if err := os.WriteFile(abs, []byte(updated), 0o644); err != nil {
		return Result{}, err
	}
	return Result{Output: "edited " + in.Path}, nil
}

// bashTool runs a command through bash inside the sandbox root, confining the
// child to its own process group so the whole group can be reaped.
type bashTool struct{}

func (bashTool) Name() string   { return "bash" }
func (bashTool) Mutating() bool { return true }

func (bashTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}

	ctx2, cancel := context.WithTimeout(ctx, DefaultCmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx2, "bash", "-lc", in.Command)
	cmd.Dir = sb.Root()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// On context cancellation (e.g. timeout) kill the entire process group
	// rather than only the leader, so descendants do not leak.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	combined, _ := cmd.CombinedOutput()

	// Defensive sweep: ensure the group is gone even on normal completion.
	// ESRCH (no such process) after exit is expected and ignored.
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	out, truncated := TruncateOutput(combined)
	if ctx2.Err() == context.DeadlineExceeded {
		out += "\n[command timed out after " + DefaultCmdTimeout.String() + "]"
	}
	return Result{Output: out, Truncated: truncated}, nil
}
