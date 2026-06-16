package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// modelsFetchTTL bounds how often a provider's /v1/models endpoint is polled.
// BuildRegistry can run per-request, so the cache makes the network fetch happen
// at most once per TTL per (providerID+baseURL) key — keeping /config/providers
// and /provider fast while still auto-populating models.
const modelsFetchTTL = 24 * time.Hour

// versionSuffixRe matches a trailing "/vN" (e.g. "/v1", "/v2") so an already
// versioned baseURL is left as-is instead of getting a second "/v1".
var versionSuffixRe = regexp.MustCompile(`/v\d+$`)

// modelModifiers are trailing leaf tokens promoted into the "(A + B)"
// parenthetical by humanizeModelID (ports the plugin MODIFIERS set).
var modelModifiers = map[string]string{
	"thinking": "Thinking",
	"agentic":  "Agentic",
	"review":   "Review",
	"none":     "None",
	"low":      "Low",
	"medium":   "Medium",
	"high":     "High",
	"xhigh":    "xHigh",
}

// modelAcronyms upper-cases known short tokens during humanization (ports the
// plugin ACRONYMS map).
var modelAcronyms = map[string]string{
	"gpt": "GPT",
	"glm": "GLM",
	"kr":  "KR",
	"cx":  "CX",
	"gh":  "GH",
	"oss": "OSS",
	"tts": "TTS",
	"stt": "STT",
	"asr": "ASR",
	"vl":  "VL",
}

// cachedModels is one TTL-bounded /v1/models result.
type cachedModels struct {
	models    []providerModel
	fetchedAt time.Time
}

// modelsCacheMu guards modelsCache. A plain mutex (not RWMutex) is enough: the
// fetch is rare (TTL-gated) and the critical sections are tiny.
var (
	modelsCacheMu sync.Mutex
	modelsCache   = map[string]cachedModels{}
)

// providerModel holds the minimal fields we need from a 9router model entry.
// "modalities" is optional - many providers only expose the ID.
type providerModel struct {
	ID         string   `json:"id"`
	Modalities []string `json:"modalities,omitempty"`
}

// cachedFetchProviderModels returns ID + optional modalities from /v1/models.
// It is additive/fail-open for config loading: fetch failures return nil and
// are not cached so a later request can retry.
func cachedFetchProviderModels(cacheKey, baseURL, apiKey string, timeout time.Duration) []providerModel {
	modelsCacheMu.Lock()
	if c, ok := modelsCache[cacheKey]; ok && time.Since(c.fetchedAt) < modelsFetchTTL {
		cached := c.models
		modelsCacheMu.Unlock()
		return cached
	}
	modelsCacheMu.Unlock()

	modelsList, err := fetchProviderModels(baseURL, apiKey, timeout)
	if err != nil {
		return nil
	}

	modelsCacheMu.Lock()
	modelsCache[cacheKey] = cachedModels{models: modelsList, fetchedAt: time.Now()}
	modelsCacheMu.Unlock()
	return modelsList
}

// fetchProviderModels GETs {normalizedBaseURL}/models and returns model IDs plus
// modalities when the list response includes them. No per-model fanout.
func fetchProviderModels(baseURL, apiKey string, timeout time.Duration) ([]providerModel, error) {
	endpoint := normalizeBaseURL(baseURL) + "/models"

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider models: status %d", resp.StatusCode)
	}

	var body struct {
		Data []providerModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	models := make([]providerModel, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID != "" {
			models = append(models, m)
		}
	}
	return models, nil
}

// normalizeBaseURL trims trailing slashes and ensures a "/vN" suffix, appending
// "/v1" when the URL is not already versioned (ports the plugin's helper).
func normalizeBaseURL(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if versionSuffixRe.MatchString(trimmed) {
		return trimmed
	}
	return trimmed + "/v1"
}

// humanizeModelID renders a model id into a display name (best-effort, display
// only), porting the plugin's humanize():
//   - strip the leading "provider/" segment
//   - pull a ":suffix" tag into a parenthetical
//   - split the leaf on "-"; trailing modifiers (thinking/agentic/review) join a
//     "(A + B)" parenthetical
//   - acronyms upper-cased, digit-leading tokens kept as-is, else capitalize
func humanizeModelID(id string) string {
	mods := []string{}

	// Strip the leading provider segment ("kr/...", "openrouter/...").
	rest := id
	if i := strings.Index(id, "/"); i != -1 {
		rest = id[i+1:]
	}

	// Pull off a ":suffix" tag (":free", ":nitro", ...) as a parenthetical.
	if colon := strings.Index(rest, ":"); colon != -1 {
		tag := strings.TrimSpace(rest[colon+1:])
		rest = rest[:colon]
		if tag != "" {
			mods = append(mods, tag)
		}
	}

	// Use only the final path component for the readable base.
	leaf := rest
	if i := strings.LastIndex(rest, "/"); i != -1 {
		leaf = rest[i+1:]
	}
	parts := strings.Split(leaf, "-")

	// Trailing word modifiers, prepended in original order.
	for len(parts) > 1 {
		last := parts[len(parts)-1]
		label, ok := modelModifiers[last]
		if !ok {
			break
		}
		parts = parts[:len(parts)-1]
		mods = append([]string{label}, mods...)
	}

	titled := make([]string, len(parts))
	for i, p := range parts {
		titled[i] = titleToken(p)
	}
	base := strings.Join(titled, " ")

	if len(mods) > 0 {
		return base + " (" + strings.Join(mods, " + ") + ")"
	}
	return base
}

// titleToken capitalizes one token for humanizeModelID: known acronyms are
// upper-cased, digit-leading version tokens are kept as-is, everything else gets
// its leading letter capitalized.
func titleToken(token string) string {
	if token == "" {
		return token
	}
	if a, ok := modelAcronyms[token]; ok {
		return a
	}
	r := []rune(token)
	if unicode.IsDigit(r[0]) {
		return token
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
