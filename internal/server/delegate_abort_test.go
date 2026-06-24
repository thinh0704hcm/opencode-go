package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"net/http"
	"net/http/httptest"

	"github.com/opencode-go/opencode-go/internal/provider"
)

type blockingDelegateProvider struct{}

func (blockingDelegateProvider) ID() string { return "mock" }

func (blockingDelegateProvider) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.ChatChunk, error) {
	out := make(chan provider.ChatChunk)
	go func() {
		defer close(out)
		<-ctx.Done()
	}()
	return out, nil
}

// waitForDelegateChild polls until the parent's first child session worker is
// running and returns its ID, failing on timeout.
func waitForDelegateChild(t *testing.T, srv *Server, parentID string) string {
	t.Helper()
	for i := 0; i < 500; i++ {
		if children := srv.store.GetSessionChildren(parentID); len(children) > 0 {
			cid := children[0].ID
			srv.sesMu.Lock()
			w, ok := srv.sesQueue[cid]
			running := ok && w != nil && w.running
			srv.sesMu.Unlock()
			if running {
				return cid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for child to start")
	return ""
}

// waitForDelegateChildStopped polls until the child worker is no longer running.
func waitForDelegateChildStopped(t *testing.T, srv *Server, childSessID string) {
	t.Helper()
	for i := 0; i < 3000; i++ {
		srv.sesMu.Lock()
		w, ok := srv.sesQueue[childSessID]
		running := true
		if ok && w != nil {
			running = w.running
		}
		srv.sesMu.Unlock()
		if !running {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("child session failed to stop")
}

func TestDelegateAbortPropagation(t *testing.T) {
	srv := New(Options{Provider: blockingDelegateProvider{}, Model: "mock"})

	parentSess := srv.store.CreateSession("", "parent", "")
	srv.store.AppendUserMessage(parentSess.ID, "", "mock", "mock", "build", []string{"run delegator"})
	_, _ = srv.store.NewAssistantMessage(parentSess.ID, "", "mock", "mock", "build", "build", false)
	ctx := withSessionID(context.Background(), parentSess.ID)

	dt := delegateTool{srv: srv}
	input := `{"prompt": "do some work", "agent": "researcher"}`

	// Foreground delegation blocks until the child finishes or is cancelled, so
	// drive it from a goroutine and cancel the child out-of-band.
	done := make(chan string, 1)
	go func() {
		res, err := dt.Execute(ctx, json.RawMessage(input), nil)
		if err != nil {
			t.Errorf("delegate failed: %v", err)
		}
		done <- res.Output
	}()

	childSessID := waitForDelegateChild(t, srv, parentSess.ID)

	// Cancel the child directly; foreground Execute must then return.
	srv.cancelSession(childSessID)
	waitForDelegateChildStopped(t, srv, childSessID)

	select {
	case out := <-done:
		if !strings.Contains(out, childSessID) {
			t.Errorf("aborted result should reference child %s, got %q", childSessID, out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("foreground delegate did not return after child cancel")
	}
}

func TestParentAbortCascades(t *testing.T) {
	srv := New(Options{Provider: blockingDelegateProvider{}, Model: "mock"})

	parentSess := srv.store.CreateSession("", "parent", "")
	srv.store.AppendUserMessage(parentSess.ID, "", "mock", "mock", "build", []string{"run delegator"})
	_, _ = srv.store.NewAssistantMessage(parentSess.ID, "", "mock", "mock", "build", "build", false)
	ctx := withSessionID(context.Background(), parentSess.ID)

	dt := delegateTool{srv: srv}
	input := `{"prompt": "do work", "agent": "researcher"}`

	// Foreground delegation blocks; run it in a goroutine then abort the parent.
	done := make(chan string, 1)
	go func() {
		res, err := dt.Execute(ctx, json.RawMessage(input), nil)
		if err != nil {
			t.Errorf("delegate failed: %v", err)
		}
		done <- res.Output
	}()

	childSessID := waitForDelegateChild(t, srv, parentSess.ID)

	// Abort the PARENT via HTTP; the cascade must stop the child.
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/session/"+parentSess.ID+"/abort", "application/json", nil)
	if err != nil {
		t.Fatalf("abort request error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("abort parent HTTP status %d", resp.StatusCode)
	}
	var ok bool
	if err := json.NewDecoder(resp.Body).Decode(&ok); err != nil {
		t.Fatalf("abort response decode: %v", err)
	}
	if !ok {
		t.Fatal("abort returned false, want true")
	}

	waitForDelegateChildStopped(t, srv, childSessID)

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("foreground delegate did not return after parent abort")
	}
}

