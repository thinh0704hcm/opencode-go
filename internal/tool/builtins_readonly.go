package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// readTool reads a single file's contents.
type readTool struct{}

func (readTool) Name() string   { return "read" }
func (readTool) Mutating() bool { return false }

func (readTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Path string `json:"path"`
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
	out, truncated := TruncateOutput(content)
	return Result{Output: out, Truncated: truncated}, nil
}

// lsTool lists the entries of a directory.
type lsTool struct{}

func (lsTool) Name() string   { return "ls" }
func (lsTool) Mutating() bool { return false }

func (lsTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if in.Path == "" {
		in.Path = "."
	}
	abs, err := sb.Resolve(in.Path)
	if err != nil {
		return Result{}, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return Result{}, err
	}
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Name())
		if e.IsDir() {
			b.WriteByte('/')
		}
		b.WriteByte('\n')
	}
	out, truncated := TruncateOutput([]byte(b.String()))
	return Result{Output: out, Truncated: truncated}, nil
}

// globTool matches files against a shell glob pattern, rooted at the sandbox.
type globTool struct{}

func (globTool) Name() string   { return "glob" }
func (globTool) Mutating() bool { return false }

func (globTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	matches, err := filepath.Glob(filepath.Join(sb.Root(), in.Pattern))
	if err != nil {
		return Result{}, err
	}
	var b strings.Builder
	for _, m := range matches {
		rel, err := filepath.Rel(sb.Root(), m)
		if err != nil {
			continue
		}
		if _, err := sb.Resolve(rel); err != nil {
			continue
		}
		b.WriteString(rel)
		b.WriteByte('\n')
	}
	out, truncated := TruncateOutput([]byte(b.String()))
	return Result{Output: out, Truncated: truncated}, nil
}

// grepTool scans files under a path for lines matching a regular expression.
type grepTool struct{}

func (grepTool) Name() string   { return "grep" }
func (grepTool) Mutating() bool { return false }

func (grepTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if in.Path == "" {
		in.Path = "."
	}
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return Result{}, err
	}
	abs, err := sb.Resolve(in.Path)
	if err != nil {
		return Result{}, err
	}
	var b strings.Builder
	root := sb.Root()
	_ = filepath.WalkDir(abs, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		scanner := bufio.NewScanner(f)
		lineno := 0
		for scanner.Scan() {
			lineno++
			line := scanner.Text()
			if re.MatchString(line) {
				b.WriteString(rel)
				b.WriteByte(':')
				b.WriteString(strconv.Itoa(lineno))
				b.WriteByte(':')
				b.WriteString(line)
				b.WriteByte('\n')
			}
		}
		return nil
	})
	out, truncated := TruncateOutput([]byte(b.String()))
	return Result{Output: out, Truncated: truncated}, nil
}

// itoa converts a non-negative int to its decimal string without strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
