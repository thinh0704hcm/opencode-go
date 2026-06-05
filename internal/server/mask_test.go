package server

import "testing"

// TestMaskSecretsDeep proves the deep redactor masks real secrets at any depth
// (provider apiKey AND mcp.*.environment.* values) while leaving model ids and
// env NAME arrays intact. This is the regression guard for the GET /config leak
// where mcp.tg-bot-go.environment.NINEROUTER_KEY escaped the narrow masker.
func TestMaskSecretsDeep(t *testing.T) {
	const apiSecret = "sk-AAAAAAAAAAAAAAAA"
	const envSecret = "sk-BBBBBBBBBBBBBBBB"

	out := map[string]any{
		"provider": map[string]any{
			"x": map[string]any{
				"options": map[string]any{
					"apiKey": apiSecret,
				},
				"models": map[string]any{
					"concactao/gemini": map[string]any{"id": "concactao/gemini"},
				},
			},
		},
		"mcp": map[string]any{
			"y": map[string]any{
				"environment": map[string]any{
					"SOME_KEY": envSecret,
				},
				"env": []any{"NINEROUTER_KEY"},
			},
		},
	}

	maskSecretsDeep(out)

	provX := out["provider"].(map[string]any)["x"].(map[string]any)
	if got := provX["options"].(map[string]any)["apiKey"]; got != maskedAPIKey {
		t.Errorf("provider apiKey = %q, want %q", got, maskedAPIKey)
	}

	mcpY := out["mcp"].(map[string]any)["y"].(map[string]any)
	if got := mcpY["environment"].(map[string]any)["SOME_KEY"]; got != maskedAPIKey {
		t.Errorf("mcp environment SOME_KEY = %q, want %q", got, maskedAPIKey)
	}

	// Model id under "models" must survive (it is a name, not a secret).
	models := provX["models"].(map[string]any)
	if _, ok := models["concactao/gemini"]; !ok {
		t.Errorf("model id concactao/gemini was dropped/renamed: %v", models)
	}
	if got := models["concactao/gemini"].(map[string]any)["id"]; got != "concactao/gemini" {
		t.Errorf("model id value = %q, want %q", got, "concactao/gemini")
	}

	// env NAME array must survive verbatim (these are names, not values).
	envNames := mcpY["env"].([]any)
	if len(envNames) != 1 || envNames[0] != "NINEROUTER_KEY" {
		t.Errorf("env names = %v, want [NINEROUTER_KEY]", envNames)
	}
}

// TestMaskSecretsDeepValuePatterns covers value-based masking (sk-* and Bearer)
// and empty-string presence semantics independent of key names.
func TestMaskSecretsDeepValuePatterns(t *testing.T) {
	out := map[string]any{
		"loose":  "sk-CCCCCCCCCCCCCCCC", // matches sk- pattern via value
		"header": "Bearer abc.def.ghi",  // matches Bearer prefix via value
		"plain":  "concactao/gemini",    // not a secret
		"empty":  "",                    // preserved as ""
		"name":   "sk-DDDDDDDDDDDDDDDD", // exempt key: kept despite pattern
	}

	maskSecretsDeep(out)

	if out["loose"] != maskedAPIKey {
		t.Errorf("loose sk- value = %q, want %q", out["loose"], maskedAPIKey)
	}
	if out["header"] != maskedAPIKey {
		t.Errorf("Bearer value = %q, want %q", out["header"], maskedAPIKey)
	}
	if out["plain"] != "concactao/gemini" {
		t.Errorf("plain value = %q, want unchanged", out["plain"])
	}
	if out["empty"] != "" {
		t.Errorf("empty value = %q, want \"\" preserved", out["empty"])
	}
	if out["name"] != "sk-DDDDDDDDDDDDDDDD" {
		t.Errorf("exempt key name = %q, want unchanged", out["name"])
	}
}
