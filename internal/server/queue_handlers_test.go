//go:build opencode_wip

package server

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestQueueHandlers(t *testing.T) {
	// temporary data dir
	dir, err := os.MkdirTemp("", "opencode-queue-test")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Build server with DataDir
	srv := New(Options{DataDir: dir})
	// Use in-memory server
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Create task
	payload := []byte(`{"name":"mytask"}`)
	resp, err := http.Post(ts.URL+"/queue/tasks", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status: %d", resp.StatusCode)
	}
	var task struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&task)
	resp.Body.Close()
	if task.ID == "" {
		t.Fatalf("no task id returned")
	}

	// Verify status
	resp, err = http.Get(ts.URL + "/queue/tasks/" + task.ID)
	if err != nil {
		t.Fatalf("status request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Write artifact manually to simulate listener output
	artDir := filepath.Join(dir, "queue", "artifacts")
	os.MkdirAll(artDir, 0755)
	artPath := filepath.Join(artDir, task.ID+".md")
	ioutil.WriteFile(artPath, []byte("# artifact"), 0644)
	// Record in store
	srv.queueStore.SetArtifact(task.ID, artPath)
	// Retrieve artifact
	resp, err = http.Get(ts.URL + "/queue/tasks/" + task.ID + "/artifact")
	if err != nil {
		t.Fatalf("artifact request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("artifact status: %d", resp.StatusCode)
	}
	data, _ := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if string(data) != "# artifact" {
		t.Fatalf("artifact content mismatch: %s", string(data))
	}

	// Abort task
	req, _ := http.NewRequest("POST", ts.URL+"/queue/tasks/"+task.ID+"/abort", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("abort request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("abort status: %d", resp.StatusCode)
	}
	resp.Body.Close()
	// Verify abort status
	resp, err = http.Get(ts.URL + "/queue/tasks/" + task.ID)
	if err != nil {
		t.Fatalf("status after abort request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status after abort code: %d", resp.StatusCode)
	}
	var gotTask struct {
		Status string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&gotTask)
	resp.Body.Close()
	if gotTask.Status != "aborted" {
		t.Fatalf("expected aborted status, got %s", gotTask.Status)
	}
}
