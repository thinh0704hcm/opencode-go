//go:build opencode_wip

package server

import (
	"strings"
)

func NormalizeProviderModel(providerID, modelID string) (string, string) {
	// If provider is empty, openai, or anthropic, strip any explicit openai/ or anthropic/ prefix from modelID.
	if providerID == "" || providerID == "openai" || providerID == "anthropic" {
		if strings.HasPrefix(modelID, "openai/") {
			modelID = strings.TrimPrefix(modelID, "openai/")
		} else if strings.HasPrefix(modelID, "anthropic/") {
			modelID = strings.TrimPrefix(modelID, "anthropic/")
		}
	}

	// Treat empty, openai, or anthropic providers as concactao internally.
	if providerID == "" || providerID == "openai" || providerID == "anthropic" {
		providerID = "concactao"
	}

	// Normalize if modelID has our internal prefixes
	if strings.HasPrefix(modelID, "cx/") || strings.HasPrefix(modelID, "ag/") || strings.HasPrefix(modelID, "cc/") {
		return "concactao", modelID
	}

	// Strip openai/ prefix when provider is concactao
	if providerID == "concactao" && strings.HasPrefix(modelID, "openai/") {
		modelID = strings.TrimPrefix(modelID, "openai/")
	}

	// Map bare Codex models to cx/
	if providerID == "concactao" || providerID == "" {
		switch modelID {
		case "gpt-5.5", "gpt-5.5-review",
			"gpt-5.4", "gpt-5.4-review", "gpt-5.4-mini", "gpt-5.4-mini-review",
			"gpt-5.3-codex", "gpt-5.3-codex-review", "gpt-5.3-codex-spark", "gpt-5.3-codex-spark-review":
			return "concactao", "cx/" + modelID
		}
	}

	// Handle aliased modelID: "provider/model"
	if strings.Contains(modelID, "/") {
		parts := strings.SplitN(modelID, "/", 2)
		p, m := parts[0], parts[1]
		if p == "cx" || p == "ag" || p == "cc" {
			return "concactao", modelID
		}
		// If current provider is empty or one of the internal/outer providers,
		// we allow splitting it.
		if providerID != "" && (providerID == "concactao" || providerID == "openai" || providerID == "anthropic") {
			// If the prefix is one of our known internal/outer providers, split.
			if p == "openai" || p == "anthropic" || p == "concactao" {
				return p, m
			}
		}

		// For other providers (e.g. "openrouter" or arbitrary external ones),
		// if modelID starts with the provider prefix, strip it.
		if providerID != "" && providerID != "concactao" {
			if strings.HasPrefix(modelID, providerID+"/") {
				trimmed := strings.TrimPrefix(modelID, providerID+"/")
				return providerID, trimmed
			}
			return providerID, modelID
		}

		// If provider unspecified, treat first segment as provider.
		if providerID == "" {
			return p, m
		}

		// If we don't have a provider yet, the first segment is the provider.
		return p, m
	}

	// Default to concactao for unspecified
	if providerID == "" || providerID == "concactao" || providerID == "openai" || providerID == "anthropic" {
		// Strip explicit openai/ or anthropic/ prefixes when provider unspecified
		if providerID == "" {
			if strings.HasPrefix(modelID, "openai/") {
				modelID = strings.TrimPrefix(modelID, "openai/")
			} else if strings.HasPrefix(modelID, "anthropic/") {
				modelID = strings.TrimPrefix(modelID, "anthropic/")
			}
		}

		if modelID == "claude-opus-4-6-thinking" {
			return "concactao", "ag/claude-opus-4-6-thinking"
		}
		// Map legacy/gpt models to cx/ prefix
		switch modelID {
		case "gpt-5.5", "gpt-5.5-review",
			"gpt-5.4", "gpt-5.4-review", "gpt-5.4-mini", "gpt-5.4-mini-review",
			"gpt-5.3-codex", "gpt-5.3-codex-review", "gpt-5.3-codex-spark", "gpt-5.3-codex-spark-review":
			return "concactao", "cx/" + modelID
		}
		return "concactao", modelID
	}

	return providerID, modelID
}
