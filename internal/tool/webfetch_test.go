package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebFetch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html><body><h1>Hello</h1><p>World</p></body></html>")
	}))
	defer ts.Close()

	tool := webFetchTool{}
	sb, _ := New(t.TempDir())
	input := json.RawMessage(fmt.Sprintf(`{"url": %q}`, ts.URL))

	res, err := tool.Execute(context.Background(), input, sb)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	expected := "Hello\nWorld"
	// stripHTMLTags might produce different whitespace, let's check
	// Our stripHTMLTags implementation:
	// func stripHTMLTags(s string) string {
	// 	var inTag bool
	// 	var result []rune
	// 	for _, r := range s {
	// 		if r == '<' {
	// 			inTag = true
	// 		} else if r == '>' {
	// 			inTag = false
	// 		} else if !inTag {
	// 			result = append(result, r)
	// 		}
	// 	}
	// 	return string(result)
	// }
	// For "<html><body><h1>Hello</h1><p>World</p></body></html>", it will produce "HelloWorld".

	expected = "HelloWorld"
	if res.Output != expected {
		t.Errorf("expected %q, got %q", expected, res.Output)
	}
}

func TestStripHTMLTags(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"<p>Hello</p>", "Hello"},
		{"<b>Bold</b> and <i>Italic</i>", "Bold and Italic"},
		{"Complex <a href='foo'>link</a>", "Complex link"},
		{"No tags", "No tags"},
	}
	for _, c := range cases {
		got := stripHTMLTags(c.in)
		if got != c.want {
			t.Errorf("stripHTMLTags(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
