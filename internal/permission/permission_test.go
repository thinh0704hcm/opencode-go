package permission

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPermissionPersistence(t *testing.T) {
	dir, err := os.MkdirTemp("", "permtest")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(dir)
	ppath := filepath.Join(dir, "perm.json")

	// First store, allow a permission, should persist
	s1 := NewStoreWithPath(ppath)
	s1.Allow("sess1", "toolA")
	if !s1.IsAllowed("sess1", "toolA") {
		t.Fatalf("permission not set in first store")
	}

	// New store loads from file
	s2 := NewStoreWithPath(ppath)
	if !s2.IsAllowed("sess1", "toolA") {
		t.Fatalf("permission not persisted across stores")
	}
}
