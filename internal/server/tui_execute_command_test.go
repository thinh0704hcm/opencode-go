//go:build opencode_wip

package server

import (
    "fmt"
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestTUIExecuteCommandCompact(t *testing.T) {
    srv := newTestServer()
    ts := httptest.NewServer(srv.Handler())
    defer ts.Close()

    // Create a session.
    resp, err := http.Post(ts.URL+"/session", "application/json", bytes.NewReader([]byte(`{}`)))
    if err != nil {
        t.Fatalf("create session: %v", err)
    }
    var sess struct{ ID string `json:"id"` }
    json.NewDecoder(resp.Body).Decode(&sess)
    resp.Body.Close()
    if sess.ID == "" {
        t.Fatalf("no session id returned")
    }

    // Add multiple user messages (>8) to trigger compression.
    for i := 0; i < 10; i++ {
        body := []byte(fmt.Sprintf(`{"messageID":"msg%d","parts":[{"type":"text","text":"msg%d"}]}`, i, i))
        _, err = http.Post(ts.URL+"/session/"+sess.ID+"/message", "application/json", bytes.NewReader(body))
        if err != nil {
            t.Fatalf("add message %d: %v", i, err)
        }
    }
    // Execute /compact command.
    cmdBody := []byte(`{"command":"/compact"}`)
    resp, err = http.Post(ts.URL+"/tui/execute-command", "application/json", bytes.NewReader(cmdBody))
    if err != nil {
        t.Fatalf("compact request: %v", err)
    }
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("compact status = %d", resp.StatusCode)
    }
    var result any
    json.NewDecoder(resp.Body).Decode(&result)
    resp.Body.Close()
    // Expect true boolean.
    if b, ok := result.(bool); !ok || !b {
        t.Fatalf("expected true response, got %+v", result)
    }

    // Verify a compression block was created.
    blocks := srv.store.CompressionBlocks(sess.ID)
    if len(blocks) == 0 {
        t.Fatalf("expected compression block, got none")
    }
}

func TestTUIExecuteCommandNoCompact(t *testing.T) {
    srv := newTestServer()
    ts := httptest.NewServer(srv.Handler())
    defer ts.Close()

    // Create a session.
    resp, err := http.Post(ts.URL+"/session", "application/json", bytes.NewReader([]byte(`{}`)))
    if err != nil {
        t.Fatalf("create session: %v", err)
    }
    var sess struct {
        ID string `json:"id"`
    }
    json.NewDecoder(resp.Body).Decode(&sess)
    resp.Body.Close()
    if sess.ID == "" {
        t.Fatalf("no session id returned")
    }

    // Send a non-compact command that starts with /compact prefix.
    cmdBody := []byte(`{"command":"/compactfoo"}`)
    resp, err = http.Post(ts.URL+"/tui/execute-command", "application/json", bytes.NewReader(cmdBody))
    if err != nil {
        t.Fatalf("request error: %v", err)
    }
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
    }
    var result any
    json.NewDecoder(resp.Body).Decode(&result)
    resp.Body.Close()
    if b, ok := result.(bool); !ok || !b {
        t.Fatalf("expected true boolean response, got %+v", result)
    }

    // Ensure no compression block was created.
    blocks := srv.store.CompressionBlocks(sess.ID)
    if len(blocks) != 0 {
        t.Fatalf("expected no compression blocks, got %d", len(blocks))
    }
}
