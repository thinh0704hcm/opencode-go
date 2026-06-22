package provider

import (
    "net/http"
    "os"
    "strings"
)

// OpenAIWithHeaders wraps OpenAI with additional header support for Helicone.
type OpenAIWithHeaders struct {
    *OpenAI
    headers map[string]string
}

// NewOpenAIWithHeaders builds an OpenAI provider with optional Helicone headers.
// It mirrors the legacy behavior used in tests: sets Helicone-Auth header when an API key is provided,
// sets Helicone-Target-URL to the original base, and overrides baseURL with HELICONE_BASE_URL if valid.
func NewOpenAIWithHeaders(id, baseURL, apiKey, model string, client *http.Client, _ map[string]string) *OpenAIWithHeaders {
    // base provider (ignores headers)
    o := NewOpenAI(id, baseURL, apiKey, model, client)
    // Init wrapper
    wrapper := &OpenAIWithHeaders{OpenAI: o, headers: map[string]string{}}
    // Set Helicone auth header if key provided
    if apiKey != "" {
        wrapper.headers["Helicone-Auth"] = "Bearer " + apiKey
    }
    // Set target URL header to original base URL
    wrapper.headers["Helicone-Target-URL"] = o.baseURL
    // Override base URL with env var if valid (starts with https://)
    if env := os.Getenv("HELICONE_BASE_URL"); env != "" && strings.HasPrefix(env, "https://") {
        wrapper.OpenAI.baseURL = strings.TrimRight(env, "/")
    }
    return wrapper
}
