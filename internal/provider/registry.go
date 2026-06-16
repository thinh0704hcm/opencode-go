package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/opencode-go/opencode-go/internal/config"
)

// envRefName recovers the {env:VAR} variable NAMES from the pre-interpolation
// config snapshot (config.RawNoEnv) so the registry can report env[] for each
// provider (architecture §3.3/§3.4).
var envRefName = regexp.MustCompile(`\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// ProviderInfo is the safe, secret-free metadata view of one configured
// provider served by GET /config/providers (architecture §3.4). It exposes the
// env-var NAMES referenced by the provider options instead of any resolved key.
type ProviderInfo struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Source string         `json:"source"`
	Env    []string       `json:"env"`
	Models map[string]any `json:"models"`
}

// Registry is the provider/model registry built once from a loaded config. It
// carries the secret-free provider metadata, the default model string, and the
// connected[] list computed from REAL resolved keys (env interpolation +
// auth.json) BEFORE any response redaction (architecture §3.4/§3.5/B7).
type Registry struct {
	Providers []ProviderInfo
	Default   string
	Connected []string
}

// BuildRegistry constructs the registry from a loaded config. It never returns
// nil and always returns non-nil slices so JSON encodes [] (not null).
func BuildRegistry(cfg *config.Config) *Registry {
	reg := &Registry{
		Providers: []ProviderInfo{},
		Connected: []string{},
	}
	if cfg == nil {
		return reg
	}

	reg.Default = cfg.Model()

	providerMap, _ := cfg.Raw["provider"].(map[string]any)
	noEnvMap, _ := cfg.RawNoEnv["provider"].(map[string]any)
	authKeys := authProviderKeys()

	// connectedSet collects providerIDs with a real resolved key. auth.json
	// providers count even if they are not declared in the config provider map.
	connectedSet := map[string]bool{}
	for id := range authKeys {
		connectedSet[id] = true
	}

	ids := make([]string, 0, len(providerMap))
	for id := range providerMap {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		obj, _ := providerMap[id].(map[string]any)

		name := id
		if s, ok := obj["name"].(string); ok && s != "" {
			name = s
		}

		models := map[string]any{}
		if m, ok := obj["models"].(map[string]any); ok {
			models = m
		}
		ensureModelLimits(models)

		// Auto-populate models from the provider's /v1/models endpoint when it
		// has a resolved baseURL (ports the ninerouter-models opencode plugin
		// which opencode-go cannot run). Additive + fail-open: existing entries
		// are preserved and any fetch error leaves models untouched. The fetch
		// is TTL-cached so per-request BuildRegistry calls stay fast.
		if baseURL := resolvedBaseURL(obj); baseURL != "" {
			for _, pm := range cachedFetchProviderModels(id+"|"+baseURL, baseURL, resolvedAPIKey(obj), 3*time.Second) {
				if _, exists := models[pm.ID]; exists {
					ensureModelLimits(map[string]any{pm.ID: models[pm.ID]})
					continue
				}
				entry := map[string]any{"id": pm.ID, "name": humanizeModelID(pm.ID), "limit": defaultModelLimit()}
				if len(pm.Modalities) > 0 {
					entry["modalities"] = pm.Modalities
				}
				models[pm.ID] = entry
			}
		}

		// Fallback: when the backend is unreachable and no models were fetched,
		// inject the configured default model so the TUI can still select it.
		if len(models) == 0 && reg.Default != "" {
			for i := 0; i < len(reg.Default); i++ {
				if reg.Default[i] == '/' {
					if reg.Default[:i] == id {
						mid := reg.Default[i+1:]
						models[mid] = map[string]any{
							"id":    mid,
							"name":  humanizeModelID(mid),
							"limit": defaultModelLimit(),
						}
					}
					break
				}
			}
		}

		info := ProviderInfo{
			ID:     id,
			Name:   name,
			Source: "config",
			Env:    envNames(noEnvMap[id]),
			Models: models,
		}
		reg.Providers = append(reg.Providers, info)

		// A config provider is connected when its resolved apiKey (post
		// {env:VAR} interpolation, from cfg.Raw) is non-empty.
		if resolvedAPIKey(obj) != "" {
			connectedSet[id] = true
		}
	}

	connected := make([]string, 0, len(connectedSet))
	for id := range connectedSet {
		connected = append(connected, id)
	}
	sort.Strings(connected)
	reg.Connected = connected

	return reg
}

func defaultModelLimit() map[string]any {
	return map[string]any{"context": float64(1048576), "output": float64(65536)}
}

func ensureModelLimits(models map[string]any) {
	for id, raw := range models {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		limit, ok := entry["limit"].(map[string]any)
		if !ok {
			entry["limit"] = defaultModelLimit()
			models[id] = entry
			continue
		}
		if _, ok := limit["context"]; !ok {
			limit["context"] = float64(1048576)
		}
		if _, ok := limit["output"]; !ok {
			limit["output"] = float64(65536)
		}
	}
}

// DefaultMap returns the default model as a {providerID:modelID} object, or an
// empty (non-nil) map when no default model is configured. Serializes to {}.
func (r *Registry) DefaultMap() map[string]string {
	out := map[string]string{}
	if r == nil || r.Default == "" {
		return out
	}
	// Default model is "providerID/modelID"; split on the FIRST slash so model
	// ids containing slashes (e.g. "cx/gpt-5.5") stay intact.
	for i := 0; i < len(r.Default); i++ {
		if r.Default[i] == '/' {
			out[r.Default[:i]] = r.Default[i+1:]
			return out
		}
	}
	return out
}

// resolvedAPIKey returns the interpolated apiKey value from a provider object's
// options ("" if absent/empty). Operates on cfg.Raw (post env interpolation).
func resolvedAPIKey(obj map[string]any) string {
	opts, ok := obj["options"].(map[string]any)
	if !ok {
		return ""
	}
	if s, ok := opts["apiKey"].(string); ok {
		return s
	}
	return ""
}

// resolvedBaseURL returns the interpolated options.baseURL value from a provider
// object ("" if absent/empty). Operates on cfg.Raw (post env interpolation).
func resolvedBaseURL(obj map[string]any) string {
	opts, ok := obj["options"].(map[string]any)
	if !ok {
		return ""
	}
	if s, ok := opts["baseURL"].(string); ok {
		return s
	}
	return ""
}

// envNames walks a provider's pre-interpolation subtree (config.RawNoEnv) and
// returns the sorted unique {env:VAR} variable names it references.
func envNames(v any) []string {
	set := map[string]bool{}
	collectEnvRefs(v, set)
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// collectEnvRefs recursively records every {env:VAR} name found in string
// values within v.
func collectEnvRefs(v any, set map[string]bool) {
	switch t := v.(type) {
	case string:
		for _, m := range envRefName.FindAllStringSubmatch(t, -1) {
			set[m[1]] = true
		}
	case map[string]any:
		for _, val := range t {
			collectEnvRefs(val, set)
		}
	case []any:
		for _, val := range t {
			collectEnvRefs(val, set)
		}
	}
}

// authProviderKeys reads ~/.local/share/opencode/auth.json and returns the set
// of providerIDs that have a non-empty key (mirrors the bot's auth.json flow,
// architecture §3.3). Missing/unparsable file yields an empty set.
func authProviderKeys() map[string]bool {
	out := map[string]bool{}
	home, err := os.UserHomeDir()
	if err != nil {
		return out
	}
	data, err := os.ReadFile(filepath.Join(home, ".local", "share", "opencode", "auth.json"))
	if err != nil {
		return out
	}
	var m map[string]struct {
		Type string `json:"type"`
		Key  string `json:"key"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return out
	}
	for id, v := range m {
		if v.Key != "" {
			out[id] = true
		}
	}
	return out
}
