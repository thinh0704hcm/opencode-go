package server

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/opencode-go/opencode-go/internal/config"
)

type dcpRuntimePrompts struct {
	System            string `json:"system"`
	CompressRange     string `json:"compressRange"`
	CompressMessage   string `json:"compressMessage"`
	ContextLimitNudge string `json:"contextLimitNudge"`
	TurnNudge         string `json:"turnNudge"`
	IterationNudge    string `json:"iterationNudge"`
}

var dcpDefaultPrompts = map[string]string{
	"system.md":              "You operate in a context-constrained environment. Manage context continuously to avoid buildup and preserve retrieval quality. The only tool you have for context management is `compress`. Use it to summarize closed or stale conversation sections into high-fidelity technical summaries.",
	"compress-range.md":      "Collapse a range in the conversation. Provide topic and content entries with startId, endId, and complete technical summary. Preserve decisions, files, commands, errors, tests, and next steps. Include compressed block placeholders when nesting summaries.",
	"compress-message.md":    "Compress selected individual messages. Provide topic and content entries with messageId, topic, and complete technical summary. Messages marked BLOCKED cannot be compressed.",
	"context-limit-nudge.md": "Context is growing. If a closed section exists, use compress to summarize it.",
	"turn-nudge.md":          "At a turn boundary, consider compressing stale completed work.",
	"iteration-nudge.md":     "Many iterations occurred without user input. Compress closed tool/output context if safe.",
}

func loadDCPPrompts(workdir string, cfg config.DCPConfig) dcpRuntimePrompts {
	writeDCPDefaultPrompts()
	get := func(name string) string { return dcpPromptContent(workdir, name, cfg.CustomPrompts) }
	return dcpRuntimePrompts{
		System:            get("system.md"),
		CompressRange:     get("compress-range.md"),
		CompressMessage:   get("compress-message.md"),
		ContextLimitNudge: get("context-limit-nudge.md"),
		TurnNudge:         get("turn-nudge.md"),
		IterationNudge:    get("iteration-nudge.md"),
	}
}

func dcpPromptContent(workdir, file string, custom bool) string {
	base := dcpDefaultPrompts[file]
	if !custom {
		return base
	}
	for _, dir := range dcpPromptOverrideDirs(workdir) {
		if b, err := os.ReadFile(filepath.Join(dir, file)); err == nil && strings.TrimSpace(string(b)) != "" {
			return normalizeDCPPrompt(string(b))
		}
	}
	return base
}

func dcpPromptOverrideDirs(workdir string) []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", "opencode", "dcp-prompts", "overrides"))
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		dirs = append([]string{filepath.Join(xdg, "opencode", "dcp-prompts", "overrides")}, dirs...)
	}
	if cfgDir := os.Getenv("OPENCODE_CONFIG_DIR"); cfgDir != "" {
		dirs = append(dirs, filepath.Join(cfgDir, "dcp-prompts", "overrides"))
	}
	if op := findNearestOpencodeDir(workdir); op != "" {
		dirs = append(dirs, filepath.Join(op, "dcp-prompts", "overrides"))
	}
	return dirs
}

func writeDCPDefaultPrompts() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".config", "opencode", "dcp-prompts", "defaults")
	_ = os.MkdirAll(dir, 0o755)
	for file, body := range dcpDefaultPrompts {
		path := filepath.Join(dir, file)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		_ = os.WriteFile(path, []byte(body+"\n"), 0o644)
	}
}

func findNearestOpencodeDir(workdir string) string {
	if workdir == "" {
		return ""
	}
	cur := filepath.Clean(workdir)
	for {
		candidate := filepath.Join(cur, ".opencode")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
		next := filepath.Dir(cur)
		if next == cur {
			return ""
		}
		cur = next
	}
}

func normalizeDCPPrompt(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	out := lines[:0]
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "<!--") || strings.HasPrefix(trim, "//") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
