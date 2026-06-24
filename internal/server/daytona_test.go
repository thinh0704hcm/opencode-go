// Blocked: depends on gated daytona.go sandbox handlers.
//go:build opencode_wip

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDaytonaDisabled(t *testing.T) {
	t.Setenv("DAYTONA_API_KEY", "")
	t.Setenv("DAYTONA_API_URL", "")
	s := newTestServer()
	req := httptest.NewRequest("POST", "/api/sandbox", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.handleDaytonaCreate(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestDaytonaCreateSuccess(t *testing.T) {
	// fake Daytona API server
	mux := http.NewServeMux()
	// combined handler for /api/sandbox (POST create, GET list)
	mux.HandleFunc("/api/sandbox", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if r.Header.Get("Authorization") != "Bearer testkey" {
				t.Fatalf("missing auth header")
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"id": "sb1", "status": "created"})
			return
		}
		if r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]map[string]string{{"id": "sb1", "status": "created"}})
			return
		}
		t.Fatalf("unexpected method %s", r.Method)
	})
	// status GET
	mux.HandleFunc("/api/sandbox/sb1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"id": "sb1", "status": "running"})
			return
		}
		if r.Method == "DELETE" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"result": "deleted"})
			return
		}
		t.Fatalf("unexpected method %s", r.Method)
	})
	// execute POST
	mux.HandleFunc("/api/sandbox/sb1/execute", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		var payload map[string][]string
		json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"output": "ok"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("DAYTONA_API_KEY", "testkey")
	t.Setenv("DAYTONA_API_URL", server.URL)
	s := newTestServer()
	// list
	req := httptest.NewRequest("GET", "/api/sandbox", nil)
	w := httptest.NewRecorder()
	s.handleDaytonaList(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list expected 200, got %d", w.Code)
	}
	// status
	req = httptest.NewRequest("GET", "/api/sandbox/sb1", nil)
	w = httptest.NewRecorder()
	s.handleDaytonaStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status expected 200, got %d", w.Code)
	}
	// execute
	execBody := strings.NewReader(`{"cmd":["echo","hi"]}`)
	req = httptest.NewRequest("POST", "/api/sandbox/sb1/execute", execBody)
	w = httptest.NewRecorder()
	s.handleDaytonaExecute(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("execute expected 200, got %d", w.Code)
	}
	// delete
	req = httptest.NewRequest("DELETE", "/api/sandbox/sb1", nil)
	w = httptest.NewRecorder()
	s.handleDaytonaDelete(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete expected 200, got %d", w.Code)
	}
}

func TestDaytonaInvalidID(t *testing.T) {
	t.Setenv("DAYTONA_API_KEY", "testkey")
	t.Setenv("DAYTONA_API_URL", "http://example.com")
	s := newTestServer()
	req := httptest.NewRequest("GET", "/api/sandbox/invalid!!", nil)
	w := httptest.NewRecorder()
	s.handleDaytonaStatus(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "testkey") {
		t.Fatalf("response leaked key")
	}
}

// New tests for edge cases
func TestDaytonaCreateInvalidID(t *testing.T) {
	// mock server returns sandbox without id
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sandbox", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "created"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("DAYTONA_API_KEY", "testkey")
	t.Setenv("DAYTONA_API_URL", server.URL)
	s := newTestServer()
	req := httptest.NewRequest("POST", "/api/sandbox", nil)
	w := httptest.NewRecorder()
	s.handleDaytonaCreate(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway for missing id, got %d", w.Code)
	}
}

func TestDaytonaNonJSONUpstream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sandbox", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("DAYTONA_API_KEY", "testkey")
	t.Setenv("DAYTONA_API_URL", server.URL)
	s := newTestServer()
	req := httptest.NewRequest("POST", "/api/sandbox", nil)
	w := httptest.NewRecorder()
	s.handleDaytonaCreate(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 internal error for non‑JSON, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "testkey") {
		t.Fatalf("response leaked API key")
	}
}

func TestDaytonaMalformedPath(t *testing.T) {
	t.Setenv("DAYTONA_API_KEY", "testkey")
	t.Setenv("DAYTONA_API_URL", "http://example.com")
	s := newTestServer()
	// Missing id
	req := httptest.NewRequest("GET", "/api/sandbox/", nil)
	w := httptest.NewRecorder()
	s.handleDaytonaStatus(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty id, got %d", w.Code)
	}
	// Extra segment
	req = httptest.NewRequest("GET", "/api/sandbox/sb1/extra", nil)
	w = httptest.NewRecorder()
	s.handleDaytonaStatus(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for extra path segment, got %d", w.Code)
	}
	// Execute with wrong action
	req = httptest.NewRequest("POST", "/api/sandbox/sb1/run", strings.NewReader(`{"cmd":["ls"]}`))
	w = httptest.NewRecorder()
	s.handleDaytonaExecute(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid execute action, got %d", w.Code)
	}
}

func TestDaytonaTimeout(t *testing.T) {
	// server delays response beyond timeout
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sandbox", func(w http.ResponseWriter, r *http.Request) {
		// simulate delay
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "sb1", "status": "created"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	// set short timeout
	t.Setenv("DAYTONA_API_KEY", "testkey")
	t.Setenv("DAYTONA_API_URL", server.URL)
	t.Setenv("DEV_CONTAINER_TIMEOUT", "1") // 1 second timeout
	s := newTestServer()
	req := httptest.NewRequest("POST", "/api/sandbox", nil)
	w := httptest.NewRecorder()
	s.handleDaytonaCreate(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 due to timeout, got %d", w.Code)
	}
}

func TestDaytonaBadBaseURL(t *testing.T) {
	t.Setenv("DAYTONA_API_KEY", "testkey")
	t.Setenv("DAYTONA_API_URL", "://badurl")
	s := newTestServer()
	req := httptest.NewRequest("GET", "/api/sandbox", nil)
	w := httptest.NewRecorder()
	s.handleDaytonaList(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "testkey") {
		t.Fatalf("response leaked key")
	}
}
