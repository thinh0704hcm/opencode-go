package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type providerStatus struct {
	Configured bool   `json:"configured"`
	Source     string `json:"source"`
}

type observabilityStatusOutput struct {
	Helicone providerStatus `json:"helicone,omitempty"`
	Sentry   providerStatus `json:"sentry,omitempty"`
	WakaTime providerStatus `json:"wakatime,omitempty"`
}

type observabilityStatusTool struct{}

func (observabilityStatusTool) Name() string   { return "observability_status" }
func (observabilityStatusTool) Mutating() bool { return false }

func NewObservabilityStatusTool() Tool { return observabilityStatusTool{} }

func (observabilityStatusTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	_ = input // no input used
	// Helper to check env vars for each provider
	check := func(keys ...string) providerStatus {
		for _, k := range keys {
			if v := os.Getenv(k); strings.TrimSpace(v) != "" {
				return providerStatus{Configured: true, Source: "env"}
			}
		}
		return providerStatus{Configured: false, Source: ""}
	}
	// import strings
	// note: we need to import strings above
	out := observabilityStatusOutput{
		Helicone: check("HELICONE_API_KEY", "HELICONE_BASE_URL"),
		Sentry:   check("SENTRY_DSN", "SENTRY_AUTH_TOKEN"),
		WakaTime: check("WAKATIME_API_KEY"),
	}
	b, err := json.Marshal(out)
	if err != nil {
		return Result{}, fmt.Errorf("marshal error: %w", err)
	}
	return Result{Output: string(b)}, nil
}
