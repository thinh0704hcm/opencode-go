package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type wakatimeHeartbeatInput struct {
	Project   string `json:"project"`
	Entity    string `json:"entity"`
	Language  string `json:"language"`
	Timestamp int64  `json:"timestamp,omitempty"`
	Send      bool   `json:"send,omitempty"`
}

type wakatimeHeartbeatOutput struct {
	Accepted bool   `json:"accepted"`
	DryRun   bool   `json:"dryRun"`
	Target   string `json:"target"`
	Details  struct {
		Project  string `json:"project"`
		Entity   string `json:"entity"`
		Language string `json:"language"`
		Time     string `json:"time"`
	} `json:"details"`
}

type wakatimeHeartbeatTool struct{}

func (wakatimeHeartbeatTool) Name() string   { return "wakatime_heartbeat" }
func (wakatimeHeartbeatTool) Mutating() bool { return false }

func NewWakatimeHeartbeatTool() Tool { return wakatimeHeartbeatTool{} }

func (wakatimeHeartbeatTool) Execute(ctx context.Context, input json.RawMessage, sb *Sandbox) (Result, error) {
	var in wakatimeHeartbeatInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.Project) == "" {
		return Result{}, fmt.Errorf("wakatime_heartbeat: project required")
	}
	if strings.TrimSpace(in.Entity) == "" {
		return Result{}, fmt.Errorf("wakatime_heartbeat: entity required")
	}
	// Build output; optionally send to WakaTime API if requested and API key present.
	out := wakatimeHeartbeatOutput{Accepted: true, DryRun: true, Target: "wakatime"}
	out.Details.Project = in.Project
	out.Details.Entity = in.Entity
	out.Details.Language = in.Language
	ts := in.Timestamp
	if ts == 0 {
		ts = time.Now().Unix()
	}
	out.Details.Time = time.Unix(ts, 0).UTC().Format(time.RFC3339)
	if in.Send {
		apiKey := os.Getenv("WAKATIME_API_KEY")
		baseURL := os.Getenv("WAKATIME_BASE_URL")
		if apiKey != "" {
			// Real POST to WakaTime endpoint
			endpoint := "https://wakatime.com/api/v1/users/current/heartbeats"
			if baseURL != "" {
				// Validate base URL
				if u, err := url.Parse(baseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
					return Result{}, fmt.Errorf("invalid WAKATIME_BASE_URL: %s", baseURL)
				}
				endpoint = strings.TrimRight(baseURL, "/") + "/api/v1/users/current/heartbeats"
			}
			payload := map[string]string{"project": in.Project, "entity": in.Entity, "language": in.Language, "time": out.Details.Time}
			body, _ := json.Marshal(payload)
			// Configurable timeout (env WAKATIME_TIMEOUT, 1-30s, default 10s)
			timeoutSec := 10
			if tsStr := os.Getenv("WAKATIME_TIMEOUT"); tsStr != "" {
				if v, err := strconv.Atoi(tsStr); err == nil {
					if v < 1 {
						v = 1
					} else if v > 30 {
						v = 30
					}
					timeoutSec = v
				}
			}
			ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
			if err != nil {
				out.DryRun = true
				out.Accepted = false
			} else {
				req.Header.Set("Authorization", "Bearer "+apiKey)
				req.Header.Set("Content-Type", "application/json")
				client := &http.Client{}
				resp, err := client.Do(req)
				if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
					out.DryRun = false
					out.Accepted = true
				} else {
					out.DryRun = true
					out.Accepted = false
				}
				if resp != nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return Result{}, fmt.Errorf("marshal error: %w", err)
	}
	return Result{Output: string(b)}, nil
}
