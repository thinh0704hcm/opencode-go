package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencode-go/opencode-go/internal/config"
)

// ResolveDefault derives a usable OpenAI-compatible provider configuration from
// the loaded opencode config plus auth.json, for use when the OPENCODE_GO_* env
// vars are unset. It takes cfg.Model() ("providerID/modelID", split on the FIRST
// slash) and looks up cfg.Raw["provider"][providerID]["options"] for baseURL and
// apiKey (apiKey already env-interpolated). If the config apiKey is empty it
// falls back to the providerID's key in ~/.local/share/opencode/auth.json.
//
// ok is true only when both baseURL and apiKey resolve to non-empty values.
func ResolveDefault(cfg *config.Config) (baseURL, apiKey, providerID, modelID string, headers map[string]string, ok bool) {
	if cfg == nil {
		return "", "", "", "", nil, false
	}

	model := cfg.Model()
	if model == "" {
		return "", "", "", "", nil, false
	}
	// Split on the FIRST slash so model ids containing slashes (e.g.
	// "cx/gpt-5.5") stay intact in modelID.
	if i := strings.Index(model, "/"); i >= 0 {
		providerID = model[:i]
		modelID = model[i+1:]
	} else {
		// No providerID prefix; nothing to look up in the provider map.
		return "", "", "", "", nil, false
	}

	providerMap, _ := cfg.Raw["provider"].(map[string]any)
	obj, _ := providerMap[providerID].(map[string]any)

	opts, _ := obj["options"].(map[string]any)
	if opts != nil {
		if s, ok := opts["baseURL"].(string); ok {
			baseURL = s
		}
		if s, ok := opts["apiKey"].(string); ok {
			apiKey = s
		}
		if h, ok := opts["headers"].(map[string]any); ok {
			headers = make(map[string]string)
			for k, v := range h {
				if sv, ok2 := v.(string); ok2 {
					headers[k] = sv
				}
			}
		}
	}

	if apiKey == "" {
		apiKey = authProviderKey(providerID)
	}

	if baseURL == "" {
		return "", "", "", "", nil, false
	}
	return baseURL, apiKey, providerID, modelID, headers, true
}

// authProviderKey reads ~/.local/share/opencode/auth.json and returns the key
// VALUE for the given providerID ("" if missing/unparsable/empty). Mirrors the
// auth.json read in authProviderKeys but returns the key string itself.
func authProviderKey(providerID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".local", "share", "opencode", "auth.json"))
	if err != nil {
		return ""
	}
	var m map[string]struct {
		Type string `json:"type"`
		Key  string `json:"key"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return m[providerID].Key
}
