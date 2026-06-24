package provider

import (
	"github.com/opencode-go/opencode-go/internal/config"
	"testing"
)

func TestResolveDefault(t *testing.T) {
    // Test case: Authorization header provided, no apiKey
    c := &config.Config{
        Raw: map[string]any{
            "provider": map[string]any{
                "myprovider": map[string]any{
                    "options": map[string]any{
                        "baseURL": "https://api.test.com",
                        
                        "headers": map[string]any{
                            "Authorization": "Bearer my-token",
                        },
                    },
                },
            },
            "model": "myprovider/gpt-4",
        },
    }
    baseURL, apiKey, providerID, modelID, headers, ok := ResolveDefault(c)


	if !ok {
		t.Fatal("expected ResolveDefault to be ok")
	}
	if baseURL != "https://api.test.com" {
		t.Errorf("expected baseURL https://api.test.com, got %s", baseURL)
	}
	if providerID != "myprovider" {
		t.Errorf("expected providerID myprovider, got %s", providerID)
	}
	if modelID != "gpt-4" {
		t.Errorf("expected modelID gpt-4, got %s", modelID)
	}
	if headers["Authorization"] != "Bearer my-token" {
		t.Errorf("expected Authorization Bearer my-token, got %s", headers["Authorization"])
	}
	if apiKey != "" {
		t.Errorf("expected empty apiKey, got %s", apiKey)
	}
}
