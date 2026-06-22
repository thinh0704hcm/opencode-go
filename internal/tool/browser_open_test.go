package tool

import (
	"testing"
)

func TestBrowserOpenToolNoop(t *testing.T) {
	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	r := NewDefaultRegistry()
	res, err := runTool(t, r, "browser_open", sb, map[string]string{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("browser_open: %v", err)
	}
	if res.Output != "noop" {
		t.Errorf("expected noop, got %s", res.Output)
	}
}

func TestBrowserOpenToolInvalidURL(t *testing.T) {
	sb, _ := New(t.TempDir())
	r := NewDefaultRegistry()
	_, err := runTool(t, r, "browser_open", sb, map[string]string{"url": "ftp://bad"})
	if err == nil {
		t.Fatalf("expected error for invalid URL")
	}
}
