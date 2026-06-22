package tool

import (
    "net/http"
    "net/http/httptest"
    "os"
    "testing"
)

func TestAsyncWakaHeartbeatSendsRequest(t *testing.T) {
    // Setup test server.
    received := false
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        received = true
        w.WriteHeader(http.StatusOK)
    }))
    defer ts.Close()

    os.Setenv("WAKATIME_API_KEY", "dummy")
    os.Setenv("WAKATIME_BASE_URL", ts.URL)
    defer func() {
        os.Unsetenv("WAKATIME_API_KEY")
        os.Unsetenv("WAKATIME_BASE_URL")
    }()

    // Call directly (synchronous). No goroutine needed here.
    asyncWakaHeartbeat("testentity")

    if !received {
        t.Fatalf("expected heartbeat request to be sent")
    }
}

func TestAsyncWakaHeartbeatNoKey(t *testing.T) {
    os.Unsetenv("WAKATIME_API_KEY")
    received := false
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        received = true
        w.WriteHeader(http.StatusOK)
    }))
    defer ts.Close()
    os.Setenv("WAKATIME_BASE_URL", ts.URL)
    defer os.Unsetenv("WAKATIME_BASE_URL")

    asyncWakaHeartbeat("entity")
    if received {
        t.Fatalf("heartbeat should not be sent without API key")
    }
}
