package provider

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchProviderModelsUsesV1ModelsAndModalities(t *testing.T) {
	var path string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"kr/test-model","modalities":["text","image"]},{"id":""}]}`))
	}))
	defer ts.Close()

	models, err := fetchProviderModels(ts.URL, "key", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", path)
	}
	if len(models) != 1 || models[0].ID != "kr/test-model" || len(models[0].Modalities) != 2 {
		t.Fatalf("models = %#v", models)
	}
}

func TestHumanizeModelIDReasoningVariants(t *testing.T) {
	cases := map[string]string{
		"cx/gpt-5.5-xhigh":                   "GPT 5.5 (xHigh)",
		"openai/gpt-5.4-medium":              "GPT 5.4 (Medium)",
		"gemini/gemini-3.1-pro-preview-high": "Gemini 3.1 Pro Preview (High)",
		"cerebras/gpt-oss-120b-low":          "GPT OSS 120b (Low)",
		"cx/gpt-5.5-xhigh-review":            "GPT 5.5 (xHigh + Review)",
	}
	for id, want := range cases {
		if got := humanizeModelID(id); got != want {
			t.Fatalf("humanizeModelID(%q) = %q, want %q", id, got, want)
		}
	}
}
