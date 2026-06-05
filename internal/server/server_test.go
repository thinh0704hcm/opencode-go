package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/opencode-go/opencode-go/internal/provider"
)

func newTestServer() *Server {
	return New(Options{Provider: provider.NewMock("hi there"), Model: "mock"})
}

// sseFrame is the decoded /global/event envelope used by the E2E test.
type sseFrame struct {
	Directory string          `json:"directory"`
	Payload   json.RawMessage `json:"payload"`
}

func TestHealthJSON(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	for _, path := range []string{"/global/health", "/api/global/health"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", path, resp.StatusCode)
		}
		var hr healthResponse
		if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
			t.Fatalf("%s decode: %v", path, err)
		}
		resp.Body.Close()
		if !hr.Healthy || hr.Version != Version {
			t.Fatalf("%s body = %+v", path, hr)
		}
	}
}

func TestSessionCreateReturnsSesID(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/session", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got struct {
		ID   string `json:"id"`
		Time struct {
			Created int64 `json:"created"`
			Updated int64 `json:"updated"`
		} `json:"time"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.ID, "ses_") {
		t.Fatalf("id = %q, want ses_ prefix", got.ID)
	}
	if got.Time.Created == 0 {
		t.Fatal("time.created not set")
	}
}

func TestPermissionReplyUnknown404(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/permission/per_nope/reply", "application/json", strings.NewReader(`{"reply":"once"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("primary reply status = %d, want 404", resp.StatusCode)
	}

	resp2, err := http.Post(ts.URL+"/session/ses_x/permissions/per_nope", "application/json", strings.NewReader(`{"response":"once"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("fallback reply status = %d, want 404", resp2.StatusCode)
	}
}

func TestEndToEndMockPrompt(t *testing.T) {
	srv := New(Options{Provider: provider.NewMock("hello world"), Model: "mock"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Create a session.
	resp, err := http.Post(ts.URL+"/session", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	var sess struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&sess)
	resp.Body.Close()
	if sess.ID == "" {
		t.Fatal("no session id")
	}

	// Subscribe to /global/event before prompting.
	streamResp, err := http.Get(ts.URL + "/global/event?directory=/work")
	if err != nil {
		t.Fatal(err)
	}
	defer streamResp.Body.Close()

	// Collect events in the background.
	events := make(chan sseFrame, 256)
	go func() {
		scanner := bufio.NewScanner(streamResp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var f sseFrame
			if err := json.Unmarshal([]byte(data), &f); err != nil {
				continue
			}
			events <- f
		}
	}()

	// Wait for server.connected.
	waitForType(t, events, "server.connected", "/work")

	// Send prompt_async.
	body := `{"model":{"providerID":"mock","modelID":"mock"},"agent":"build","parts":[{"type":"text","text":"hi"}]}`
	preq, _ := http.NewRequest(http.MethodPost, ts.URL+"/session/"+sess.ID+"/prompt_async", strings.NewReader(body))
	preq.Header.Set("Content-Type", "application/json")
	presp, err := http.DefaultClient.Do(preq)
	if err != nil {
		t.Fatal(err)
	}
	presp.Body.Close()
	if presp.StatusCode != http.StatusNoContent {
		t.Fatalf("prompt_async status = %d, want 204", presp.StatusCode)
	}

	// Assert the event sequence.
	sawDelta := false
	sawUpdated := false
	sawFinalMsg := false
	sawIdle := false
	deadline := time.After(5 * time.Second)
loop:
	for {
		select {
		case f := <-events:
			var pe struct {
				Type       string          `json:"type"`
				Properties json.RawMessage `json:"properties"`
			}
			json.Unmarshal(f.Payload, &pe)
			switch pe.Type {
			case "message.part.delta":
				var p struct {
					SessionID string `json:"sessionID"`
					Field     string `json:"field"`
				}
				json.Unmarshal(pe.Properties, &p)
				if p.Field == "text" && p.SessionID == sess.ID {
					sawDelta = true
				}
			case "message.part.updated":
				var p struct {
					SessionID string `json:"sessionID"`
					Part      struct {
						Type string `json:"type"`
					} `json:"part"`
				}
				json.Unmarshal(pe.Properties, &p)
				if p.Part.Type == "text" && p.SessionID == sess.ID {
					sawUpdated = true
				}
			case "message.updated":
				var p struct {
					Info struct {
						Role string `json:"role"`
						Time struct {
							Completed *int64 `json:"completed"`
						} `json:"time"`
					} `json:"info"`
				}
				json.Unmarshal(pe.Properties, &p)
				if p.Info.Role == "assistant" && p.Info.Time.Completed != nil {
					sawFinalMsg = true
				}
			case "session.idle":
				sawIdle = true
				break loop
			}
		case <-deadline:
			t.Fatal("timed out waiting for event sequence")
		}
	}

	if !sawDelta {
		t.Error("missing message.part.delta (field text)")
	}
	if !sawUpdated {
		t.Error("missing message.part.updated (part text)")
	}
	if !sawFinalMsg {
		t.Error("missing final assistant message.updated with time.completed")
	}
	if !sawIdle {
		t.Error("missing synthetic session.idle")
	}

	// GET /session/{id}/message returns the assistant text.
	mresp, err := http.Get(ts.URL + "/session/" + sess.ID + "/message")
	if err != nil {
		t.Fatal(err)
	}
	defer mresp.Body.Close()
	var msgs []struct {
		Info struct {
			Role string `json:"role"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	json.NewDecoder(mresp.Body).Decode(&msgs)
	var asstText string
	for _, m := range msgs {
		if m.Info.Role == "assistant" {
			for _, p := range m.Parts {
				if p.Type == "text" {
					asstText += p.Text
				}
			}
		}
	}
	if asstText != "hello world" {
		t.Fatalf("assistant text = %q, want %q", asstText, "hello world")
	}
}

func waitForType(t *testing.T, events <-chan sseFrame, typ, wantDir string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f := <-events:
			if wantDir != "" && f.Directory != wantDir {
				t.Fatalf("envelope directory = %q, want %q", f.Directory, wantDir)
			}
			var pe struct {
				Type string `json:"type"`
			}
			json.Unmarshal(f.Payload, &pe)
			if pe.Type == typ {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", typ)
		}
	}
}
