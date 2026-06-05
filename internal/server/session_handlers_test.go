package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencode-go/opencode-go/internal/provider"
)

// createSession POSTs an empty body and returns the new session id.
func createSession(t *testing.T, baseURL string) string {
	t.Helper()
	resp, err := http.Post(baseURL+"/session", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID == "" {
		t.Fatal("no session id")
	}
	return got.ID
}

// doRequest issues a request with the given method/body and returns status+body.
func doRequest(t *testing.T, method, url, body string) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, b
}

func TestSessionGet(t *testing.T) {
	srv := New(Options{Provider: provider.NewMock("hi"), Model: "mock"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createSession(t, ts.URL)

	// 200 after create.
	status, body := doRequest(t, http.MethodGet, ts.URL+"/session/"+id, "")
	if status != http.StatusOK {
		t.Fatalf("GET known session status = %d, want 200", status)
	}
	var sess struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &sess); err != nil {
		t.Fatal(err)
	}
	if sess.ID != id {
		t.Fatalf("GET session id = %q, want %q", sess.ID, id)
	}

	// 404 unknown.
	status, _ = doRequest(t, http.MethodGet, ts.URL+"/session/ses_nope", "")
	if status != http.StatusNotFound {
		t.Fatalf("GET unknown session status = %d, want 404", status)
	}
}

func TestSessionUpdateTitle(t *testing.T) {
	srv := New(Options{Provider: provider.NewMock("hi"), Model: "mock"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createSession(t, ts.URL)

	status, body := doRequest(t, http.MethodPatch, ts.URL+"/session/"+id, `{"title":"renamed"}`)
	if status != http.StatusOK {
		t.Fatalf("PATCH status = %d, want 200", status)
	}
	var sess struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(body, &sess); err != nil {
		t.Fatal(err)
	}
	if sess.Title != "renamed" {
		t.Fatalf("title = %q, want %q", sess.Title, "renamed")
	}

	// 404 unknown.
	status, _ = doRequest(t, http.MethodPatch, ts.URL+"/session/ses_nope", `{"title":"x"}`)
	if status != http.StatusNotFound {
		t.Fatalf("PATCH unknown status = %d, want 404", status)
	}
}

func TestSessionDelete(t *testing.T) {
	srv := New(Options{Provider: provider.NewMock("hi"), Model: "mock"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createSession(t, ts.URL)

	status, body := doRequest(t, http.MethodDelete, ts.URL+"/session/"+id, "")
	if status != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200", status)
	}
	var ok bool
	if err := json.Unmarshal(body, &ok); err != nil {
		t.Fatalf("DELETE body decode: %v (body=%q)", err, string(body))
	}
	if !ok {
		t.Fatal("DELETE returned false, want true")
	}

	// GET after delete -> 404.
	status, _ = doRequest(t, http.MethodGet, ts.URL+"/session/"+id, "")
	if status != http.StatusNotFound {
		t.Fatalf("GET after delete status = %d, want 404", status)
	}

	// DELETE unknown -> 404.
	status, _ = doRequest(t, http.MethodDelete, ts.URL+"/session/ses_nope", "")
	if status != http.StatusNotFound {
		t.Fatalf("DELETE unknown status = %d, want 404", status)
	}
}

func TestSessionChildrenEmpty(t *testing.T) {
	srv := New(Options{Provider: provider.NewMock("hi"), Model: "mock"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createSession(t, ts.URL)

	status, body := doRequest(t, http.MethodGet, ts.URL+"/session/"+id+"/children", "")
	if status != http.StatusOK {
		t.Fatalf("GET children status = %d, want 200", status)
	}
	var children []json.RawMessage
	if err := json.Unmarshal(body, &children); err != nil {
		t.Fatalf("children decode: %v (body=%q)", err, string(body))
	}
	if len(children) != 0 {
		t.Fatalf("children len = %d, want 0", len(children))
	}

	// 404 unknown.
	status, _ = doRequest(t, http.MethodGet, ts.URL+"/session/ses_nope/children", "")
	if status != http.StatusNotFound {
		t.Fatalf("GET children unknown status = %d, want 404", status)
	}
}

func TestSessionAbort(t *testing.T) {
	srv := New(Options{Provider: provider.NewMock("hi"), Model: "mock"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createSession(t, ts.URL)

	status, body := doRequest(t, http.MethodPost, ts.URL+"/session/"+id+"/abort", "")
	if status != http.StatusOK {
		t.Fatalf("POST abort status = %d, want 200", status)
	}
	var ok bool
	if err := json.Unmarshal(body, &ok); err != nil {
		t.Fatalf("abort body decode: %v (body=%q)", err, string(body))
	}
	if !ok {
		t.Fatal("abort returned false, want true")
	}

	// 404 unknown.
	status, _ = doRequest(t, http.MethodPost, ts.URL+"/session/ses_nope/abort", "")
	if status != http.StatusNotFound {
		t.Fatalf("POST abort unknown status = %d, want 404", status)
	}
}

func TestSessionGetSingleMessage(t *testing.T) {
	srv := New(Options{Provider: provider.NewMock("hi"), Model: "mock"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Drive the store directly for a deterministic message id (white-box).
	sess := srv.store.CreateSession("", "", "")
	msg, ok := srv.store.AppendUserMessage(sess.ID, "", []string{"hello"})
	if !ok {
		t.Fatal("AppendUserMessage failed")
	}
	messageID := msg.Info.ID

	// 200 for known message.
	status, body := doRequest(t, http.MethodGet, ts.URL+"/session/"+sess.ID+"/message/"+messageID, "")
	if status != http.StatusOK {
		t.Fatalf("GET message status = %d, want 200", status)
	}
	var got struct {
		Info struct {
			ID string `json:"id"`
		} `json:"info"`
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Info.ID != messageID {
		t.Fatalf("message id = %q, want %q", got.Info.ID, messageID)
	}
	if len(got.Parts) != 1 || got.Parts[0].Text != "hello" {
		t.Fatalf("parts = %+v, want one text 'hello'", got.Parts)
	}

	// 404 unknown message.
	status, _ = doRequest(t, http.MethodGet, ts.URL+"/session/"+sess.ID+"/message/msg_nope", "")
	if status != http.StatusNotFound {
		t.Fatalf("GET unknown message status = %d, want 404", status)
	}
}
