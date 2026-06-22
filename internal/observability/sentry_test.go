package observability

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

)

func TestCaptureErrorValidEndpoint(t *testing.T) {
    // Setup test server to capture request.
    received := false
    var receivedMsg string
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        received = true
        var payload map[string]string
        if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
            t.Fatalf("decode error: %v", err)
        }
        receivedMsg = payload["message"]
        w.WriteHeader(http.StatusOK)
    }))
    defer ts.Close()

    // Set env variables.
    os.Setenv("SENTRY_ENDPOINT", ts.URL)
    defer os.Unsetenv("SENTRY_ENDPOINT")
    // Use error containing a faux bearer token.
    err := fmt.Errorf("something failed: Bearer secretToken123")
    CaptureError(err)

    if !received {
        t.Fatalf("expected request to be sent")
    }
    if strings.Contains(receivedMsg, "secretToken123") {
        t.Fatalf("secret token not redacted in payload: %s", receivedMsg)
    }
    if !strings.Contains(receivedMsg, "Bearer <redacted>") {
        t.Fatalf("expected redacted bearer in payload: %s", receivedMsg)
    }
}

func TestCaptureErrorInvalidScheme(t *testing.T) {
    // Set invalid scheme.
    os.Setenv("SENTRY_ENDPOINT", "ftp://invalid.example.com")
    defer os.Unsetenv("SENTRY_ENDPOINT")
    // No server should be hit; just ensure no panic.
    defer func() {
        if r := recover(); r != nil {
            t.Fatalf("CaptureError panicked with invalid scheme")
        }
    }()
    CaptureError(fmt.Errorf("test error"))
}
