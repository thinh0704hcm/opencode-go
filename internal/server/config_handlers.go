package server

import (
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/opencode-go/opencode-go/internal/config"
	"github.com/opencode-go/opencode-go/internal/provider"
)

// maskedAPIKey is the non-empty sentinel substituted for any configured
// provider apiKey before a /config* or /provider* response is serialized
// (architecture §3.5/B7). It is non-empty so the TUI treats the provider as
// "has a key" without the real secret ever crossing the wire.
const maskedAPIKey = "***"

// handleConfigGet serves GET /config: the merged, env-interpolated config with
// the boot-required keys back-filled ($schema, command, agent, mode, plugin,
// username, model) and every provider.*.options.apiKey masked.
func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	cfg := config.Load(directoryOf(r))
	out := cfg.Defaulted()
	maskSecretsDeep(out)
	writeJSON(w, http.StatusOK, out)
}

// handleConfigUpdate serves PATCH /config.
func (s *Server) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	s.handleTUIOK(w, r)
}

// configProvidersResponse is the GET /config/providers body.
type configProvidersResponse struct {
	Providers []provider.ProviderInfo `json:"providers"`
	Default   map[string]string       `json:"default"`
}

// handleConfigProviders serves GET /config/providers: the secret-free provider
// metadata list (env[] names kept, apiKey never present) plus the default model
// as a {providerID:modelID} object.
func (s *Server) handleConfigProviders(w http.ResponseWriter, r *http.Request) {
	reg := provider.BuildRegistry(config.Load(directoryOf(r)))
	writeJSON(w, http.StatusOK, configProvidersResponse{
		Providers: reg.Providers,
		Default:   reg.DefaultMap(),
	})
}

// providerListResponse is the GET /provider body.
type providerListResponse struct {
	All       []provider.ProviderInfo `json:"all"`
	Default   map[string]string       `json:"default"`
	Connected []string                `json:"connected"`
}

// handleProvider serves GET /provider: all providers, the default model object,
// and connected[] (computed from REAL resolved keys BEFORE redaction; the
// ProviderInfo list is already secret-free).
func (s *Server) handleProvider(w http.ResponseWriter, r *http.Request) {
	reg := provider.BuildRegistry(config.Load(directoryOf(r)))
	writeJSON(w, http.StatusOK, providerListResponse{
		All:       reg.Providers,
		Default:   reg.DefaultMap(),
		Connected: reg.Connected,
	})
}

// agentInfo is one entry in the GET /agent response.
type agentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Mode        string `json:"mode"`
	Model       any    `json:"model,omitempty"`
	Native      bool   `json:"native,omitempty"`
}

// handleAgent serves GET /agent: the file-based agents loaded via loadAgents,
// always including the default "build" agent first (architecture §7.2).
func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	out := []agentInfo{{Name: "build", Description: "The default agent.", Mode: "primary", Native: true}}
	workdir := directoryOf(r)
	if workdir == "" {
		workdir = s.workdir
	}
	agents := loadAgents(workdir)
	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		a := agents[name]
		if a.Name == "build" {
			continue // don't duplicate the default
		}
		mode := a.Mode
		if mode == "" {
			mode = "primary"
		}
		out = append(out, agentInfo{Name: a.Name, Description: a.Description, Mode: mode, Model: agentModelObject(a.Model)})
	}
	writeJSON(w, http.StatusOK, out)
}

func agentModelObject(model string) any {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	idx := strings.Index(model, "/")
	if idx <= 0 || idx == len(model)-1 {
		return nil
	}
	return map[string]string{"providerID": model[:idx], "modelID": model[idx+1:]}
}

// secretValueRe flags string VALUES that are themselves secrets regardless of
// the key holding them: an OpenAI-style "sk-" token (>=12 trailing chars) or an
// inline "Bearer " credential.
var secretValueRe = regexp.MustCompile(`(?i)sk-[A-Za-z0-9_-]{12,}`)

// googleKeyRe flags Google API keys ("AIza" prefix + >=20 token chars). Kept
// case-sensitive to avoid false positives on ordinary config strings.
var googleKeyRe = regexp.MustCompile(`AIza[0-9A-Za-z_-]{20,}`)

// secretKeyNames are map KEYS (compared case-insensitively) whose string VALUE
// is always a secret and must be masked.
var secretKeyNames = map[string]bool{
	"apikey":        true,
	"api_key":       true,
	"key":           true,
	"token":         true,
	"secret":        true,
	"password":      true,
	"authorization": true,
	"bearer":        true,
}

// neverMaskKeyNames are map KEYS (case-insensitive) that hold names/metadata,
// never secrets. Their string VALUES are left intact even if they would
// otherwise match a secret key name or value pattern. Recursion still descends
// into their non-string children.
var neverMaskKeyNames = map[string]bool{
	"env":        true,
	"models":     true,
	"connected":  true,
	"default":    true,
	"source":     true,
	"name":       true,
	"id":         true,
	"modelid":    true,
	"providerid": true,
}

// valueLooksSecret reports whether a string VALUE is itself a credential.
func valueLooksSecret(s string) bool {
	return strings.HasPrefix(s, "Bearer ") || secretValueRe.MatchString(s) || googleKeyRe.MatchString(s)
}

// maskSecretsDeep recursively walks maps and slices, replacing secret string
// values with the maskedAPIKey sentinel so no real credential escapes any
// /config* response. A string is masked when (a) its map KEY is a known secret
// key name, or (b) its VALUE matches a secret pattern. Keys in neverMaskKeyNames
// are exempt (they carry names/metadata). Every string VALUE inside an
// "environment"/"env" MAP (NAME->VALUE) is masked while the NAME keys are kept.
// Empty strings are preserved to keep "configured vs missing" presence semantics.
func maskSecretsDeep(v any) {
	switch node := v.(type) {
	case map[string]any:
		for k, val := range node {
			lk := strings.ToLower(k)

			// (4) environment/env as a NAME->VALUE map: mask all values, keep names.
			if lk == "environment" || lk == "env" {
				if envMap, ok := val.(map[string]any); ok {
					for ek, ev := range envMap {
						if sv, ok := ev.(string); ok {
							if sv != "" {
								envMap[ek] = maskedAPIKey
							}
							continue
						}
						maskSecretsDeep(ev)
					}
					continue
				}
			}

			// Exempt keys: never mask their string value, but still descend.
			if neverMaskKeyNames[lk] {
				maskSecretsDeep(val)
				continue
			}

			if sv, ok := val.(string); ok {
				if sv == "" {
					continue
				}
				if secretKeyNames[lk] || valueLooksSecret(sv) {
					node[k] = maskedAPIKey
				}
				continue
			}
			maskSecretsDeep(val)
		}
	case []any:
		for _, item := range node {
			maskSecretsDeep(item)
		}
	}
}
