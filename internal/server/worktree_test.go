//go:build opencode_wip

package server

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/opencode-go/opencode-go/internal/session"
)

func TestWorktreeAddAndPersistence(t *testing.T) {
	// Setup temporary HOME and workdir.
	home := t.TempDir()
	t.Setenv("HOME", home)
	workdir := t.TempDir()

	// Expected base directory for worktrees.
	base := filepath.Join(home, ".local", "share", "opencode", "worktrees")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	wtPath := filepath.Join(base, "wt1")
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("mkdir wt: %v", err)
	}

	reg := NewWorktreeRegistry(workdir, "")
	if _, err := reg.Add(wtPath); err != nil {
		t.Fatalf("add worktree: %v", err)
	}
	// Verify persisted file exists.
	if _, err := os.Stat(reg.file); err != nil {
		t.Fatalf("persisted file missing: %v", err)
	}
	// Reload registry and ensure worktree persists.
	reg2 := NewWorktreeRegistry(workdir, "")
	list := reg2.List()
	if len(list) != 1 || list[0].Path != wtPath {
		t.Fatalf("persisted worktree mismatch: %+v", list)
	}
}

func TestWorktreePathEscapes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workdir := t.TempDir()
	base := filepath.Join(home, ".local", "share", "opencode", "worktrees")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	// Attempt to add a path that escapes the base via "..".
	escapePath := filepath.Join(base, "..", "outside")
	reg := NewWorktreeRegistry(workdir, "")
	if _, err := reg.Add(escapePath); err == nil {
		t.Fatalf("expected error for escaped path, got nil")
	}
}

func TestAssignAndReset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workdir := t.TempDir()
	base := filepath.Join(home, ".local", "share", "opencode", "worktrees")
	os.MkdirAll(base, 0o755)
	wtPath := filepath.Join(base, "wt2")
	os.MkdirAll(wtPath, 0o755)

	store := session.NewStore()
	sess := store.CreateSession("", "", "test", "")

	reg := NewWorktreeRegistry(workdir, "")
	if _, err := reg.Add(wtPath); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := reg.Assign(store, sess.ID, wtPath); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if dir := store.SessionWorkdir(sess.ID); dir != wtPath {
		t.Fatalf("session dir not set, got %q", dir)
	}
	// Verify worktree records session.
	found := false
	for _, wt := range reg.List() {
		if wt.Path == wtPath && wt.SessionID == sess.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("worktree session not recorded")
	}
	// Reset
	if err := reg.Reset(store, sess.ID); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if dir := store.SessionWorkdir(sess.ID); dir != "" {
		t.Fatalf("session dir not cleared, got %q", dir)
	}
	// Ensure worktree session cleared.
	for _, wt := range reg.List() {
		if wt.Path == wtPath && wt.SessionID != "" {
			t.Fatalf("worktree session not cleared")
		}
	}
}

func TestConcurrentAdd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workdir := t.TempDir()
	base := filepath.Join(home, ".local", "share", "opencode", "worktrees")
	os.MkdirAll(base, 0o755)

	reg := NewWorktreeRegistry(workdir, "")
	var wg sync.WaitGroup
	const n = 20
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			path := filepath.Join(base, "wt_conc_"+strconv.Itoa(i))
			os.MkdirAll(path, 0o755)
			_, _ = reg.Add(path) // ignore duplicate errors
		}(i)
	}
	wg.Wait()
	list := reg.List()
	if len(list) != n {
		t.Fatalf("expected %d worktrees, got %d", n, len(list))
	}
}
