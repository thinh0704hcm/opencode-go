package tool

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowserOpenFetch(t *testing.T) {
	// mock server returns simple body and checks User-Agent
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		if ua != "opencode-go/1.0" {
			t.Fatalf("unexpected User-Agent %s", ua)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello world"))
	}))
	defer ts.Close()

	sb, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("sandbox: %v", err)
	}
	r := NewDefaultRegistry()
	in := map[string]string{"url": ts.URL, "mode": "fetch"}
	res, err := runTool(t, r, "browser_open", sb, in)
	if err != nil {
		t.Fatalf("tool error: %v", err)
	}
	// output should be body
	if res.Output != "hello world" {
		t.Fatalf("unexpected output %s", res.Output)
	}
}
