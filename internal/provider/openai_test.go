package provider

import (
    "os"
    "testing"
    "strings"
)

func TestHeliconeHeadersAndBaseURL(t *testing.T) {
    // Clean env after.
    origKey := os.Getenv("HELICONE_API_KEY")
    origBase := os.Getenv("HELICONE_BASE_URL")
    defer func() {
        os.Setenv("HELICONE_API_KEY", origKey)
        os.Setenv("HELICONE_BASE_URL", origBase)
    }()

    os.Setenv("HELICONE_API_KEY", "mykey123")
    os.Setenv("HELICONE_BASE_URL", "https://proxy.example.com")

    o := NewOpenAIWithHeaders("test", "https://api.openai.com", "apikey", "gpt-4", nil, nil)

    if !strings.HasPrefix(o.headers["Helicone-Auth"], "Bearer ") {
        t.Fatalf("Helicone-Auth missing Bearer prefix: %s", o.headers["Helicone-Auth"])
    }
    if o.headers["Helicone-Target-URL"] != "https://api.openai.com" {
        t.Fatalf("Helicone-Target-URL not set correctly, got %s", o.headers["Helicone-Target-URL"])
    }
    if o.baseURL != "https://proxy.example.com" {
        t.Fatalf("baseURL not overridden correctly, got %s", o.baseURL)
    }
}

func TestHeliconeInvalidBaseURL(t *testing.T) {
    os.Setenv("HELICONE_API_KEY", "key")
    os.Setenv("HELICONE_BASE_URL", "ftp://invalid")
    defer func() {
        os.Unsetenv("HELICONE_API_KEY")
        os.Unsetenv("HELICONE_BASE_URL")
    }()
    o := NewOpenAIWithHeaders("test", "https://api.openai.com", "apikey", "gpt-4", nil, nil)
    if o.baseURL != "https://api.openai.com" {
        t.Fatalf("expected baseURL unchanged on invalid Helicone base, got %s", o.baseURL)
    }
    if _, ok := o.headers["Helicone-Target-URL"]; ok {
        t.Fatalf("Helicone-Target-URL should not be set on invalid base URL")
    }
}
