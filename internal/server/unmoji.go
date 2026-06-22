//go:build opencode_wip

package server

import (
	"strings"
)

func isEmoji(r rune) bool {
	switch {
	case r >= 0x1F300 && r <= 0x1FAFF:
		return true
	case r >= 0x2600 && r <= 0x27BF:
		return true
	case r >= 0x1F000 && r <= 0x1F0FF:
		return true
	case r >= 0x2300 && r <= 0x23FF:
		return true
	case r >= 0xFE00 && r <= 0xFE0F:
		return true
	case r == 0x200D:
		return true
	case r >= 0x1F1E6 && r <= 0x1F1FF:
		return true
	default:
		return false
	}
}

// Unmoji removes emojis and normalizes whitespace.
func Unmoji(s string) string {
	var b strings.Builder
	for _, r := range s {
		if isEmoji(r) {
			continue
		}
		b.WriteRune(r)
	}
	cleaned := strings.Join(strings.Fields(b.String()), " ")
	return cleaned
}
