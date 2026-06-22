package config

// DCPConfig holds configuration for Dynamic Context Pruning.
type DCPConfig struct {
    Enabled            bool     `json:"enabled"`
    Mode               string   `json:"mode"`               // "range" | "compact"
    ProtectUserMessages bool   `json:"protectUserMessages"`
    ProtectedTools    []string `json:"protectedTools"`
    ErrorPruneTurns   int      `json:"errorPruneTurns"`
    TurnNudgeInterval int      `json:"turnNudgeInterval"`
    ManualMode        bool     `json:"manualMode"`
    CompressPermission string  `json:"compressPermission"` // "allow" | "deny"
    CustomPrompts     bool     `json:"customPrompts"`
    Auto               bool     `json:"auto"` // auto-compaction enabled
    ContextLimit      int      `json:"contextLimit"` // model context window tokens
    OutputLimit       int      `json:"outputLimit"` // max output tokens
}

