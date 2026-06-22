package tool

import (
	"context"
	"os"
	"testing"
)

func TestMemoryRemoteBaseURLValidation(t *testing.T) {
	os.Setenv("SUPERMEMORY_API_KEY", "dummy")
	os.Setenv("SUPERMEMORY_BASE_URL", "ftp://invalid")
	defer func() {
		os.Unsetenv("SUPERMEMORY_API_KEY")
		os.Unsetenv("SUPERMEMORY_BASE_URL")
	}()
	// invoke Execute with remote true expecting error
	// minimal stub sandbox
	_, err := memoryTool{}.Execute(context.Background(), []byte(`{"action":"list","remote":true}`), nil)
	if err == nil {
		t.Fatalf("expected error for invalid base URL")
	}
}

// SandboxMock provides minimal implementation for testing
type SandboxMock struct{}

func (s *SandboxMock) Root() string { return "" }
