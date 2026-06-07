package server

import (
	_ "embed"
	"fmt"
	"runtime"
	"strings"
	"time"
)

//go:embed prompt_default.txt
var defaultSystemPrompt string

// buildSystemPrompt returns the Claude-Code-style default prompt followed by an
// <env> block grounding the model in the current workspace, OS, and date.
func buildSystemPrompt(workdir, basePrompt string) string {
	base := defaultSystemPrompt
	if strings.TrimSpace(basePrompt) != "" {
		base = basePrompt
	}
	env := fmt.Sprintf(
		"<env>\nWorking directory: %s\nPlatform: %s\nToday's date: %s\n</env>",
		workdir, runtime.GOOS, time.Now().Format("2006-01-02"),
	)
	prompt := base + "\n\n" + env
	if skills := loadSkillIndex(workdir); skills != "" {
		prompt += "\n\n" + skills
	}
	return prompt
}
