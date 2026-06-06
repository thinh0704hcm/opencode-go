package server

import (
	_ "embed"
	"fmt"
	"runtime"
	"time"
)

//go:embed prompt_default.txt
var defaultSystemPrompt string

// buildSystemPrompt returns the Claude-Code-style default prompt followed by an
// <env> block grounding the model in the current workspace, OS, and date.
func buildSystemPrompt(workdir string) string {
	env := fmt.Sprintf(
		"<env>\nWorking directory: %s\nPlatform: %s\nToday's date: %s\n</env>",
		workdir, runtime.GOOS, time.Now().Format("2006-01-02"),
	)
	return defaultSystemPrompt + "\n\n" + env
}
