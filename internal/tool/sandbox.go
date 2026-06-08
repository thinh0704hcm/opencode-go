package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Output-cap configuration.
const (
	// MaxOutputBytes is the maximum number of bytes of command output retained.
	MaxOutputBytes = 64 * 1024
	// DefaultCmdTimeout is the default timeout for sandboxed command execution.
	DefaultCmdTimeout = 120 * time.Second
)

// truncationMarker is inserted between the retained head and tail when output
// exceeds MaxOutputBytes.
const truncationMarker = "\n...truncated...\n"

// Sandbox confines path resolution to a single realpath-resolved root.
type Sandbox struct {
	root string // realpath-resolved absolute root
}

// New cleans and absolutizes root, resolves any symlinks (root must already
// exist), and stores the result. It returns an error if root cannot be
// absolutized or does not exist.
func New(root string) (*Sandbox, error) {
	abs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolve root: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("sandbox: root must exist: %w", err)
	}
	return &Sandbox{root: real}, nil
}

// Root returns the realpath-resolved absolute root of the sandbox.
func (s *Sandbox) Root() string {
	return s.root
}

// OpenFileNoFollow resolves rel within the sandbox and opens it with
// syscall.O_NOFOLLOW set on the final component, so a symlink swapped in at the
// terminal path between Resolve's check and this open is rejected rather than
// followed. This closes the residual TOCTOU gap documented on Resolve for
// callers that adopt it.
func (s *Sandbox) OpenFileNoFollow(rel string, flag int, perm os.FileMode) (*os.File, error) {
	abs, err := s.Resolve(rel)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(abs, flag|syscall.O_NOFOLLOW, perm)
}

// Resolve maps a relative path to a safe absolute path inside the sandbox
// root. It allows creating new files (paths that do not yet exist, as long as
// their parent exists) but rejects absolute paths, traversal that escapes the
// root, and symlink escapes.
//
// Residual TOCTOU note: Resolve validates symlinks at check time, but the
// actual file operation (e.g. os.ReadFile/os.WriteFile in the calling tools)
// runs later on the returned path. A symlink swapped between this check and the
// subsequent use could escape the sandbox. This window is low risk on a
// single-tenant loopback deployment, but callers that need to close it should
// open the final path via OpenFileNoFollow, which sets syscall.O_NOFOLLOW on
// the final component so an attacker-swapped terminal symlink is rejected at
// open time rather than silently followed.
func (s *Sandbox) Resolve(rel string) (string, error) {
	// 1. Reject absolute inputs or ".." elements that escape the root.
	if filepath.IsAbs(rel) {
		// Be forgiving: if the absolute path is within the sandbox root, make it relative.
		if strings.HasPrefix(filepath.Clean(rel), s.root+string(os.PathSeparator)) || filepath.Clean(rel) == s.root {
			var err error
			rel, err = filepath.Rel(s.root, rel)
			if err != nil {
				return "", fmt.Errorf("sandbox: absolute path not allowed: %q", rel)
			}
		} else {
			return "", fmt.Errorf("sandbox: absolute path escapes root: %q", rel)
		}
	}
	if escapesRoot(rel) {
		return "", fmt.Errorf("sandbox: path escapes sandbox: %q", rel)
	}

	// 2. Clean the joined path.
	clean := filepath.Clean(filepath.Join(s.root, rel))

	// 3. Resolve symlinks for the target if it exists; otherwise resolve the
	//    parent (which must exist) and re-attach the final element.
	var real string
	if _, err := os.Lstat(clean); err == nil {
		real, err = filepath.EvalSymlinks(clean)
		if err != nil {
			return "", fmt.Errorf("sandbox: resolve path: %w", err)
		}
	} else {
		realParent, perr := filepath.EvalSymlinks(filepath.Dir(clean))
		if perr != nil {
			return "", fmt.Errorf("sandbox: parent must exist: %w", perr)
		}
		real = filepath.Join(realParent, filepath.Base(clean))
	}

	// 4. Require the resolved path to be the root itself or within it.
	if real != s.root && !strings.HasPrefix(real, s.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("sandbox: path escapes sandbox: %q", rel)
	}
	return real, nil
}

// escapesRoot reports whether a cleaned relative path would resolve outside of
// the root via ".." elements.
func escapesRoot(rel string) bool {
	cleaned := filepath.Clean(rel)
	if cleaned == ".." {
		return true
	}
	return strings.HasPrefix(cleaned, ".."+string(os.PathSeparator))
}

// TruncateOutput caps b to MaxOutputBytes. When b is over the cap, it keeps a
// head and tail joined by a truncation marker, and reports true. Otherwise it
// returns b unchanged and reports false.
func TruncateOutput(b []byte) (string, bool) {
	if len(b) <= MaxOutputBytes {
		return string(b), false
	}

	budget := MaxOutputBytes - len(truncationMarker)
	if budget <= 0 {
		// Marker alone exceeds the cap; return a hard-capped slice.
		return string(b[:MaxOutputBytes]), true
	}

	head := budget / 2
	tail := budget - head
	var sb strings.Builder
	sb.Grow(head + len(truncationMarker) + tail)
	sb.Write(b[:head])
	sb.WriteString(truncationMarker)
	sb.Write(b[len(b)-tail:])
	return sb.String(), true
}
