package server

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/opencode-go/opencode-go/internal/config"
	"github.com/opencode-go/opencode-go/internal/provider"
)

var defaultDCPProtectedTools = []string{"task", "skill", "todowrite", "todoread", "compress", "batch", "plan_enter", "plan_exit", "write", "edit"}

func applyDCPStrategies(messages []provider.ChatMessage, cfg config.DCPConfig) []provider.ChatMessage {
    out := cloneChatMessages(messages)
    protectedStart := len(out)
    if cfg.TurnNudgeInterval > 0 && len(out) > cfg.TurnNudgeInterval {
        protectedStart = len(out) - cfg.TurnNudgeInterval
    }
    if !cfg.ManualMode {
        // deduplication always enabled unless manual mode
        out = dedupeToolResults(out, appendProtected(defaultDCPProtectedTools, cfg.ProtectedTools), protectedStart)
        // purge errors
        turns := cfg.ErrorPruneTurns
        if turns <= 0 {
            turns = 4
        }
        out = purgeErroredToolInputs(out, appendProtected(defaultDCPProtectedTools, cfg.ProtectedTools), turns, protectedStart)
    }
    return out
}


func cloneChatMessages(messages []provider.ChatMessage) []provider.ChatMessage {
	out := make([]provider.ChatMessage, len(messages))
	copy(out, messages)
	for i := range out {
		if len(out[i].ToolCalls) > 0 {
			tcs := make([]provider.ChatToolCall, len(out[i].ToolCalls))
			copy(tcs, out[i].ToolCalls)
			out[i].ToolCalls = tcs
		}
	}
	return out
}

func dedupeToolResults(messages []provider.ChatMessage, protected []string, protectedStart int) []provider.ChatMessage {
	latest := map[string]int{}
	for i, m := range messages {
		if i >= protectedStart || m.Role != "assistant" || len(m.ToolCalls) != 1 {
			continue
		}
		for _, tc := range m.ToolCalls {
			if toolProtected(tc.Function.Name, protected) {
				continue
			}
			latest[toolSignature(tc)] = i
		}
	}
	remove := map[int]bool{}
	seen := map[string]bool{}
	for i, m := range messages {
		if i >= protectedStart || m.Role != "assistant" || len(m.ToolCalls) != 1 {
			continue
		}
		for _, tc := range m.ToolCalls {
			sig := toolSignature(tc)
			if toolProtected(tc.Function.Name, protected) || seen[sig] {
				continue
			}
			seen[sig] = true
			if latest[sig] != i {
				remove[i] = true
				for j := i + 1; j < len(messages) && messages[j].Role == "tool"; j++ {
					if messages[j].ToolCallID == tc.ID {
						remove[j] = true
					}
				}
			}
		}
	}
	if len(remove) == 0 {
		return messages
	}
	out := make([]provider.ChatMessage, 0, len(messages)-len(remove))
	for i, m := range messages {
		if !remove[i] {
			out = append(out, m)
		}
	}
	return out
}

func purgeErroredToolInputs(messages []provider.ChatMessage, protected []string, turns int, protectedStart int) []provider.ChatMessage {
	threshold := len(messages) - turns
	if protectedStart < threshold {
		threshold = protectedStart
	}
	if threshold <= 0 {
		return messages
	}
	errorByCall := map[string]bool{}
	for i, m := range messages {
		if i >= threshold || m.Role != "tool" {
			continue
		}
		if strings.Contains(strings.ToLower(contentString(m.Content)), "error") {
			errorByCall[m.ToolCallID] = true
		}
	}
	for i := range messages {
		if i >= threshold || messages[i].Role != "assistant" {
			continue
		}
		for j := range messages[i].ToolCalls {
			tc := &messages[i].ToolCalls[j]
			if errorByCall[tc.ID] && !toolProtected(tc.Function.Name, protected) {
				tc.Function.Arguments = "{}"
			}
		}
	}
	return messages
}

func toolSignature(tc provider.ChatToolCall) string {
	return tc.Function.Name + "::" + normalizeJSONString(tc.Function.Arguments)
}

func normalizeJSONString(s string) string {
	var v any
	if json.Unmarshal([]byte(s), &v) != nil {
		return s
	}
	b, _ := json.Marshal(sortJSON(v))
	return string(b)
}

func sortJSON(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(t))
		for _, k := range keys {
			out[k] = sortJSON(t[k])
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = sortJSON(t[i])
		}
		return out
	default:
		return v
	}
}

func appendProtected(base []string, extras ...[]string) []string {
	out := append([]string{}, base...)
	for _, xs := range extras {
		out = append(out, xs...)
	}
	return out
}

func toolProtected(name string, patterns []string) bool {
	for _, p := range patterns {
		if p == name {
			return true
		}
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}

func contentString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []provider.ContentPart:
		var b strings.Builder
		for _, p := range t {
			b.WriteString(p.Text)
		}
		return b.String()
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
