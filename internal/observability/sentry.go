package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"os"
	"time"
)

// CaptureError sends error info to Sentry endpoint if configured. Never panics.
func CaptureError(err error) {
	if err == nil {
		return
	}
	// Resolve endpoint: prioritize SENTRY_ENDPOINT, fallback to DSN if valid.
	dsn := os.Getenv("SENTRY_DSN")
	endpoint := os.Getenv("SENTRY_ENDPOINT")
	urlStr := ""
	if endpoint != "" {
		urlStr = endpoint
	} else if dsn != "" {
		urlStr = dsn
	} else {
		return
	}
	// Validate URL scheme (http/https).
	if u, parseErr := url.ParseRequestURI(urlStr); parseErr != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return
	}
	// Sanitize error message: remove possible secrets (simple regex for bearer tokens).
	msg := err.Error()
	// Example pattern: "Bearer <token>"
	msg = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._-]+`).ReplaceAllString(msg, "Bearer <redacted>")
	payload := map[string]any{"message": msg}
	body, _ := json.Marshal(payload)
	// Send synchronously; caller may choose goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err2 := client.Do(req)
	if err2 != nil {
		return
	}
	if resp.Body != nil {
		io.CopyN(io.Discard, resp.Body, 64*1024)
		resp.Body.Close()
	}
}
