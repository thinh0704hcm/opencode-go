package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	if err := os.MkdirAll(filepath.Dir(filepath.Join(sb.Root(), in.Path)), 0o755); err != nil {
		return Result{}, err
	}
	f, err := sb.OpenFileNoFollow(in.Path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return Result{}, err
	}
	_, werr := f.WriteString(in.Content)
	cerr := f.Close()
	if werr != nil {
		return Result{}, werr
	}
	if cerr != nil {
		return Result{}, cerr
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
	rf, err := sb.OpenFileNoFollow(in.Path, os.O_RDONLY, 0)
	if err != nil {
		return Result{}, err
	}
	content, err := io.ReadAll(rf)
	rf.Close()
	if err != nil {
		return Result{}, err
	}
	if !strings.Contains(string(content), in.Old) {
		return Result{}, errOldNotFound
	}
	updated := strings.Replace(string(content), in.Old, in.New, 1)
	wf, err := sb.OpenFileNoFollow(in.Path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return Result{}, err
	}
	_, werr := wf.WriteString(updated)
	cerr := wf.Close()
	if werr != nil {
		return Result{}, werr
	}
	if cerr != nil {
		return Result{}, cerr
	}
	return Result{Output: "edited " + in.Path}, nil
}

// limitedWriter writes to w until n bytes have been consumed, after which
// further writes are discarded (reporting success so the producing process
// keeps running and exits normally) and truncated is flagged.
type limitedWriter struct {
	w         io.Writer
	n         int
	truncated bool
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		l.truncated = true
		return len(p), nil
	}
	if len(p) > l.n {
		l.w.Write(p[:l.n])
		l.n = 0
		l.truncated = true
		return len(p), nil
	}
	l.n -= len(p)
	return l.w.Write(p)
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

	var buf bytes.Buffer
	lw := &limitedWriter{w: &buf, n: MaxOutputBytes * 2}
	cmd.Stdout = lw
	cmd.Stderr = lw
	runErr := cmd.Run()

	// Defensive sweep: ensure the group is gone even on normal completion.
	// ESRCH (no such process) after exit is expected and ignored.
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	out, truncated := TruncateOutput(buf.Bytes())
	if ctx2.Err() == context.DeadlineExceeded {
		out += "\n[command timed out after " + DefaultCmdTimeout.String() + "]"
	} else if exitErr, ok := runErr.(*exec.ExitError); ok {
		out += "\n[exit status " + strconv.Itoa(exitErr.ExitCode()) + "]"
	}
	return Result{Output: out, Truncated: truncated}, nil
}
