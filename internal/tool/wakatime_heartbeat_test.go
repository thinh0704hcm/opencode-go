package tool

import (
	"context"
	"os"
	"testing"
)

func TestWakaTimeBaseURLValidation(t *testing.T) {
	os.Setenv("WAKATIME_BASE_URL", "invalid://url")
	defer os.Unsetenv("WAKATIME_BASE_URL")
	os.Setenv("WAKATIME_API_KEY", "dummy")
	defer os.Unsetenv("WAKATIME_API_KEY")
	_, err := wakatimeHeartbeatTool{}.Execute(context.Background(), []byte(`{"project":"p","entity":"e","send":true}`), nil)
	if err == nil {
		t.Fatalf("expected error for invalid WAKATIME_BASE_URL")
	}
}
