package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// asyncWakaHeartbeat sends a best‑effort WakaTime heartbeat for the given entity.
func asyncWakaHeartbeat(entity string) {
	apiKey := os.Getenv("WAKATIME_API_KEY")
	if apiKey == "" {
		return
	}
	baseURL := os.Getenv("WAKATIME_BASE_URL")
	endpoint := "https://wakatime.com/api/v1/users/current/heartbeats"
	if baseURL != "" {
		if u, err := url.Parse(baseURL); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			endpoint = strings.TrimRight(baseURL, "/") + "/api/v1/users/current/heartbeats"
		}
	}
	// Use current time.
	ts := time.Now().Unix()
	tStr := time.Unix(ts, 0).UTC().Format(time.RFC3339)
	payload := map[string]string{"project": "opencode-go", "entity": entity, "time": tStr}
	body, _ := json.Marshal(payload)
	// Configurable timeout via env.
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
	// Perform request synchronously; caller may run in goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	if resp.Body != nil {
		io.CopyN(io.Discard, resp.Body, 64*1024)
		resp.Body.Close()
	}
}
