//go:build opencode_wip

package server

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

type VibeGuard struct {
	enabled bool
}

func NewVibeGuard() *VibeGuard {
	val := strings.ToLower(os.Getenv("VIBE_GUARD_ENABLED"))
	en := val == "1" || val == "true"
	return &VibeGuard{enabled: en}
}

// Redact replaces secret-like patterns with placeholders and returns meta map.
func (vg *VibeGuard) Redact(b []byte) ([]byte, map[string]string) {
	if !vg.enabled {
		return b, map[string]string{}
	}
	patterns := []string{
		`sk-[A-Za-z0-9]{8,}`,
		`Bearer\s+[A-Za-z0-9._-]{8,}`,
		`AKIA[0-9A-Z]{16}`,
		`[A-Za-z0-9_-]{32,}`,
	}
	meta := make(map[string]string)
	idx := 1
	input := string(b)
	for _, pat := range patterns {
		re := regexp.MustCompile(pat)
		input = re.ReplaceAllStringFunc(input, func(m string) string {
			placeholder := "[[REDACTED:" + strconv.Itoa(idx) + "]]"
			meta[placeholder] = m
			idx++
			return placeholder
		})
	}
	return []byte(input), meta
}

// Restore replaces placeholders back to original values using meta.
func (vg *VibeGuard) Restore(b []byte, meta map[string]string) []byte {
	if !vg.enabled || len(meta) == 0 {
		return b
	}
	s := string(b)
	for ph, orig := range meta {
		s = strings.ReplaceAll(s, ph, orig)
	}
	return []byte(s)
}
