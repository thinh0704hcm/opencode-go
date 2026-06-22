package config

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
)

// Schema is the canonical $schema value the TUI expects.
const Schema = "https://opencode.ai/config.json"

// Config holds the merged, env-interpolated configuration as a generic map plus
// the directory it was resolved for (architecture §3.3).
type Config struct {
	// Raw is the merged config object (project overlay over global files),
	// with {env:VAR} interpolation already applied. Never nil.
	Raw map[string]any
	// RawNoEnv is the same merged object BEFORE {env:VAR} interpolation, used
	// by the provider registry to recover env-var NAMES for env[]. Never nil.
	RawNoEnv map[string]any
	// Directory is the ?directory= value the config was resolved against.
	Directory string
}

// Load reads and merges configuration for the given directory. The load order
// (lowest precedence first) is:
//
//	$OPENCODE_CONFIG, ./opencode.jsonc, ./.opencode/opencode.jsonc,
//	~/.config/opencode/opencode.json(c)
//
// then the project overlay from <directory>/.opencode/opencode.json(c) is
// applied last (highest precedence). Missing files are skipped. {env:VAR}
// interpolation is applied to the merged result. Load never returns nil.
func Load(directory string) *Config {
	merged := map[string]any{}

	for _, path := range loadPaths(directory) {
		if path == "" {
			continue
		}
		m, ok := readConfigFile(path)
		if !ok {
			continue
		}
		merged = mergeMaps(merged, m)
	}

	// highest-precedence overlay from SDK / CI callers
	if raw := os.Getenv("OPENCODE_CONFIG_CONTENT"); raw != "" {
		var overlay map[string]any
		if json.Unmarshal([]byte(raw), &overlay) == nil {
			merged = mergeMaps(merged, overlay)
		}
	}

	// Snapshot before interpolation so the provider registry can recover the
	// {env:VAR} NAMES for env[].
	rawNoEnv := deepCopyMap(merged)
	interpolateEnv(merged)

	return &Config{Raw: merged, RawNoEnv: rawNoEnv, Directory: directory}
}

// loadPaths returns the ordered list of candidate config file paths (lowest
// precedence first).
func loadPaths(directory string) []string {
	var paths []string

	paths = append(paths, os.Getenv("OPENCODE_CONFIG"))
	paths = append(paths, "opencode.jsonc")
	paths = append(paths, filepath.Join(".opencode", "opencode.jsonc"))

	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, ".config", "opencode", "opencode.json"),
			filepath.Join(home, ".config", "opencode", "opencode.jsonc"),
		)
	}

	// Project overlay (highest precedence) from the request directory.
	if directory != "" {
		paths = append(paths,
			filepath.Join(directory, ".opencode", "opencode.json"),
			filepath.Join(directory, ".opencode", "opencode.jsonc"),
		)
	}

	return paths
}

// readConfigFile reads, strips JSONC, and unmarshals one config file. Returns
// (nil,false) if the file is missing or fails to parse.
func readConfigFile(path string) (map[string]any, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	clean := stripJSONC(data)
	if len(strings.TrimSpace(string(clean))) == 0 {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(clean, &m); err != nil {
		return nil, false
	}
	return m, true
}

// mergeMaps deep-merges overlay onto base and returns the result. Maps are
// merged recursively; all other values (including slices) are replaced.
func mergeMaps(base, overlay map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for k, ov := range overlay {
		if bv, ok := base[k]; ok {
			bm, bok := bv.(map[string]any)
			om, ook := ov.(map[string]any)
			if bok && ook {
				base[k] = mergeMaps(bm, om)
				continue
			}
		}
		base[k] = ov
	}
	return base
}

// envRef matches {env:VAR_NAME} interpolation references.
var envRef = regexp.MustCompile(`\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// interpolateEnv walks the map and replaces {env:VAR} references in string
// values with the corresponding environment variable value (architecture §3.3).
func interpolateEnv(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if s, ok := val.(string); ok {
				t[k] = expandEnvRefs(s)
			} else {
				interpolateEnv(val)
			}
		}
	case []any:
		for i, val := range t {
			if s, ok := val.(string); ok {
				t[i] = expandEnvRefs(s)
			} else {
				interpolateEnv(val)
			}
		}
	}
}

// expandEnvRefs replaces every {env:VAR} in s with os.Getenv(VAR).
func expandEnvRefs(s string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		name := envRef.FindStringSubmatch(m)[1]
		return os.Getenv(name)
	})
}

// Defaulted returns a deep copy of the merged config with the boot-required keys
// back-filled so the TUI never sees a missing field (M2 spec /config):
// $schema, command, agent, mode, plugin, username, model. Existing values win.
func (c *Config) Defaulted() map[string]any {
	out := deepCopyMap(c.Raw)

	if _, ok := out["$schema"]; !ok {
		out["$schema"] = Schema
	}
	ensureObject(out, "command")
	ensureObject(out, "agent")
	ensureObject(out, "mode")
	if _, ok := out["plugin"]; !ok {
		out["plugin"] = []any{}
	}
	if _, ok := out["username"]; !ok {
		out["username"] = osUsername()
	}
	if _, ok := out["model"]; !ok {
		out["model"] = ""
	}

	return out
}

// Model returns the configured default model string ("providerID/modelID"), or
// "" if unset.
func (c *Config) Model() string {
    if s, ok := c.Raw["model"].(string); ok {
        return s
    }
    return ""
}

// DCP returns the DCP configuration section.
func (c Config) DCP() DCPConfig {
    // defaults per spec
    d := DCPConfig{
        Mode:               "range",
        ErrorPruneTurns:    4,
        TurnNudgeInterval:  5,
        CompressPermission: "allow",
    }
    raw, ok := c.Raw["dcp"]
    if !ok {
        return d
    }
    m, ok := raw.(map[string]any)
    if !ok {
        return d
    }
    if v, ok := m["enabled"].(bool); ok {
        d.Enabled = v
    }
    if v, ok := m["mode"].(string); ok {
        d.Mode = v
    }
    if v, ok := m["protectUserMessages"].(bool); ok {
        d.ProtectUserMessages = v
    }
    if v, ok := m["protectedTools"].([]any); ok {
        for _, t := range v {
            if s, ok := t.(string); ok {
                d.ProtectedTools = append(d.ProtectedTools, s)
            }
        }
    }
    if v, ok := m["errorPruneTurns"].(float64); ok {
        d.ErrorPruneTurns = int(v)
    }
    if v, ok := m["turnNudgeInterval"].(float64); ok {
        d.TurnNudgeInterval = int(v)
    }
    if v, ok := m["manualMode"].(bool); ok {
        d.ManualMode = v
    }
    if v, ok := m["compressPermission"].(string); ok {
        d.CompressPermission = v
    }
    if v, ok := m["customPrompts"].(bool); ok {
        d.CustomPrompts = v
    }
    if v, ok := m["auto"].(bool); ok {
        d.Auto = v
    }
    if v, ok := m["contextLimit"].(float64); ok {
        d.ContextLimit = int(v)
    }
    if v, ok := m["outputLimit"].(float64); ok {
        d.OutputLimit = int(v)
    }
    return d
}


// ensureObject guarantees key maps to a (possibly empty) JSON object.
func ensureObject(m map[string]any, key string) {
	if _, ok := m[key].(map[string]any); !ok {
		m[key] = map[string]any{}
	}
}

// osUsername returns the current OS username, falling back to $USER then "user".
func osUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		// Strip any DOMAIN\ prefix on the off chance.
		if i := strings.LastIndexAny(u.Username, `\/`); i >= 0 {
			return u.Username[i+1:]
		}
		return u.Username
	}
	if env := os.Getenv("USER"); env != "" {
		return env
	}
	return "user"
}

// deepCopyMap returns a deep copy of m (maps and slices duplicated) so callers
// can mutate the result without touching the cached config.
func deepCopyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return deepCopyMap(t)
	case []any:
		s := make([]any, len(t))
		for i, e := range t {
			s[i] = deepCopyValue(e)
		}
		return s
	default:
		return v
	}
}
