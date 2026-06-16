# PLAN-v2-pass7.md

Seventh-pass fixes. All items confirmed against post-pass-6 source and
`@opencode-ai/sdk` v2 TypeScript types.

---

## Audit: pass 6 confirmed complete

All six pass-6 items verified in current code:

| Item | File:line | Status |
|---|---|---|
| `step.started` emitted | `agent_loop.go:102` | ✅ |
| `step.ended` emitted (both paths) | `agent_loop.go:233, 365` | ✅ |
| Reasoning events emitted | `agent_loop.go:177,180,209` | ✅ |
| `FinishOpenParts` in `finishGeneration` | `generation.go:170` | ✅ |
| Reasoning parts include `time` | `v2_handlers.go:540` | ✅ |
| Permission request list real data | `v2_handlers.go:935` | ✅ |
| Busy-session replay prompted/admitted | `v2_handlers.go:851-863` | ✅ |
| FSList/FSRead real implementations | `v2_handlers.go:1015,1031` | ✅ |

---

## Critical

### Fix 1 — `AppendStepFinish` never called; `step-finish` parts missing from store

**File:** `internal/server/generation.go`

`store.AppendStepFinish(sessionID, messageID, reason, cost, tokens)` exists at
`store.go:531` and `mapToV2Message` renders `step-finish` parts at
`v2_handlers.go:590`. But nothing in the current code calls `AppendStepFinish`.

Consequence: `GET /api/session/{id}/messages` responses never include a
`step-finish` content item. The TUI reads `step-finish.cost` and
`step-finish.tokens` to show per-turn cost after page reload or on session
restore. These values are always missing.

The backup `generation.go.bak-20260611-agent-metadata:101` shows the intended
call site: inside `runGenerationSyncCtx` right before `finishGeneration`.

Fix — add to `runGenerationSyncCtx` after `runAgentLoop` returns:

```go
finishReason := s.runAgentLoop(ctx, sessionID, asst.Info.ID, parentID, modelID, texts, images, system, agent)

// Record terminal reason and compute final step cost for the step-finish part.
reason := finishReason
if reason == "" {
    reason = "stop"
}
aborted := ctx.Err() != nil
if aborted {
    reason = "aborted"
}

var stepTokens *session.Tokens
var stepCost float64
if info, ok := s.store.MessageInfo(sessionID, asst.Info.ID); ok && info.Tokens != nil {
    stepTokens = info.Tokens
    stepCost = computeCost(info.ModelID, info.Tokens.Input, info.Tokens.Output)
}
if stepTokens == nil {
    stepTokens = &session.Tokens{}
}
if sf, ok := s.store.AppendStepFinish(sessionID, asst.Info.ID, reason, stepCost, stepTokens); ok {
    s.bus.Publish(event.NewMessagePartUpdated(sessionID, sf, time.Now().UnixMilli()))
}

s.finishGeneration(sessionID, asst.Info.ID)
return s.store.GetMessage(sessionID, asst.Info.ID)
```

Also add the `computeCost` import — it is already defined in
`internal/server/pricing.go`.

The `aborted` path does NOT need special handling for idle events because
`cancelSession` / `handleSessionAbort` already publishes them.

---

### Fix 2 — Initial step-start part not published via SSE on turn start

**File:** `internal/server/generation.go:runGenerationSyncCtx`

`store.NewAssistantMessage` auto-creates a `step-start` part (store.go:364)
but `runGenerationSyncCtx` only publishes `message.updated` for the info, not
`message.part.updated` for the initial part. The TUI step animation requires
seeing `message.part.updated{type:"step-start"}` before any text or tool parts.

Fix — after creating the assistant message, stream its initial step-start part:

```go
asst, ok := s.store.NewAssistantMessage(...)
if !ok {
    return session.MessageWithParts{}, false
}
s.bus.Publish(event.NewMessageUpdated(sessionID, asst.Info, false))
// Publish the auto-created step-start part so the TUI renders the step marker.
if len(asst.Parts) > 0 {
    s.bus.Publish(event.NewMessagePartUpdated(sessionID, asst.Parts[0], time.Now().UnixMilli()))
}
```

---

## Correctness

### Fix 3 — `mapToV2Message` text parts missing `time` and `metadata`

**File:** `internal/server/v2_handlers.go:524`

SDK `TextPart` (types.gen.d.ts:243) has optional `time?: {start, end?}` and
`metadata?`. Our `text` case emits only `{type, id, text}`. The `Part` struct
already stores `.Time` (used by reasoning parts) and `.Metadata` (used by
tool parts) — the data is there, just not emitted.

Fix: add time/metadata output to the `text` case, parallel to `reasoning`:

```go
case "text":
    tp := map[string]any{
        "type": "text",
        "id":   p.ID,
        "text": p.Text,
    }
    if p.Time != nil {
        var endMS any
        if p.Time.End != nil {
            endMS = *p.Time.End
        }
        tp["time"] = map[string]any{"start": p.Time.Start, "end": endMS}
    }
    if p.Metadata != nil {
        tp["metadata"] = p.Metadata
    }
    content = append(content, tp)
```

---

### Fix 4 — `session.next.step.ended` emits cumulative tokens, not per-step

**File:** `internal/server/agent_loop.go:222–234, 356–366`

Both `step.ended` emit paths read `info.Tokens` via `s.store.MessageInfo`.
`SetAssistantUsage` is called on every usage chunk and overwrites the
message-level counters — so by the end of step N, `info.Tokens` holds the
cumulative total across all N steps, not just step N's tokens.

For a single-step turn this is fine. For multi-step tool-calling turns, every
`step.ended` event shows the total accumulated tokens instead of the delta
for that step.

Fix: track a `prevTokens` snapshot at the top of the `for` loop, compute the
delta at step end:

```go
var prevInput, prevOutput int64

for {
    s.bus.Publish(event.NewSessionNextStepStarted(...))
    stepStartInput  := prevInput
    stepStartOutput := prevOutput

    // ... stream ...

    // After stream + tool execution, compute delta:
    var tokens event.SessionNextStepEndedTokens
    var stepCost float64
    if info, ok := s.store.MessageInfo(sessionID, messageID); ok && info.Tokens != nil {
        tokens.Input  = info.Tokens.Input  - stepStartInput
        tokens.Output = info.Tokens.Output - stepStartOutput
        tokens.Cache.Read  = info.Tokens.Cache.Read
        tokens.Cache.Write = info.Tokens.Cache.Write
        stepCost = computeCost(modelID, tokens.Input, tokens.Output)
        prevInput  = info.Tokens.Input
        prevOutput = info.Tokens.Output
    }
    s.bus.Publish(event.NewSessionNextStepEnded(sessionID, messageID, finishReason, stepCost, tokens))
```

Note: `computeCost` is defined in `pricing.go` and already accessible from
`agent_loop.go` (same `server` package).

---

### Fix 5 — `handleV2FSList` / `handleV2FSRead` path-traversal vulnerability

**File:** `internal/server/v2_handlers.go:1009, 1031`

Both handlers accept a raw `?path=` query parameter and pass it directly to
`os.ReadDir` / `os.ReadFile`. Any path on the server is readable, including
`/etc/passwd`, `/root/.ssh/id_rsa`, etc.

Fix: restrict to descendants of `s.workdir` using `filepath.Clean` + prefix
check, identical to how `tool/sandbox.go` works:

```go
func (s *Server) resolveWorkdirPath(rawPath string) (string, error) {
    if rawPath == "" {
        return s.workdir, nil
    }
    abs := rawPath
    if !filepath.IsAbs(abs) {
        abs = filepath.Join(s.workdir, abs)
    }
    clean := filepath.Clean(abs)
    if clean != s.workdir && !strings.HasPrefix(clean, s.workdir+string(filepath.Separator)) {
        return "", fmt.Errorf("path outside workdir")
    }
    return clean, nil
}
```

Apply to both `handleV2FSList` and `handleV2FSRead`:

```go
func (s *Server) handleV2FSList(w http.ResponseWriter, r *http.Request) {
    path, err := s.resolveWorkdirPath(r.URL.Query().Get("path"))
    if err != nil {
        writeError(w, http.StatusForbidden, err.Error())
        return
    }
    // ... rest unchanged
}
```

Add `"fmt"` import if not already present (it should already be there).

---

### Fix 6 — `chatHistory` drops tool-call structures from completed turns

**File:** `internal/server/agent_loop.go:30`

`chatHistory` reconstructs provider history from persisted messages using only
`partsText(msg.Parts, "text")`. Completed assistant messages that had tool
calls are re-presented to the provider as plain text turns — the tool call
structures and tool results are lost from history.

This means a follow-up prompt after a tool-using turn gives the provider a
history like:

```
user: "what's in /tmp?"
assistant: ""          ← tool call output lost; text was empty
user: "summarize it"
```

Instead of:

```
user: "what's in /tmp?"
assistant: [tool_call: ls /tmp]
tool: [result: ...]
assistant: "Here are the files..."
user: "summarize it"
```

Fix: for assistant messages, check for tool parts and reconstruct the
`[tool_calls] + [tool_results]` pair in the history:

```go
if role == "assistant" {
    toolParts := toolPartsOf(msg.Parts)
    if len(toolParts) > 0 {
        // Reconstruct: assistant(tool_calls) + tool results + assistant(final text)
        tcs := make([]provider.ChatToolCall, 0, len(toolParts))
        for _, tp := range toolParts {
            inputJSON, _ := json.Marshal(tp.State.Input)
            tcs = append(tcs, provider.ChatToolCall{
                ID:   tp.CallID,
                Type: "function",
                Function: provider.ChatToolCallFunction{
                    Name:      tp.Tool,
                    Arguments: string(inputJSON),
                },
            })
        }
        out = append(out, provider.ChatMessage{Role: "assistant", ToolCalls: tcs})
        for _, tp := range toolParts {
            output := ""
            if tp.State != nil {
                output = tp.State.Output
            }
            out = append(out, provider.ChatMessage{
                Role: "tool", ToolCallID: tp.CallID, Name: tp.Tool, Content: output,
            })
        }
    }
    // Always include the final text turn (may be empty if purely tool-calling).
    text := partsText(msg.Parts, "text")
    if text != "" || len(toolParts) == 0 {
        out = append(out, provider.ChatMessage{Role: role, Content: provider.TextContent(text)})
    }
    continue
}
```

Add a helper:

```go
func toolPartsOf(parts []session.Part) []session.Part {
    var out []session.Part
    for _, p := range parts {
        if p.Type == "tool" && p.State != nil && p.State.Status != "running" {
            out = append(out, p)
        }
    }
    return out
}
```

Also add `"encoding/json"` to `agent_loop.go` imports if missing — it's
already there (used for `json.Unmarshal` on tool inputs).

---

## Implementation order

1. Fix 2 — publish initial step-start part (one-liner in `generation.go`)
2. Fix 1 — call `AppendStepFinish` in `runGenerationSyncCtx`
3. Fix 3 — text part `time`/`metadata` in `mapToV2Message`
4. Fix 5 — path-traversal guard for `FSList`/`FSRead`
5. Fix 4 — per-step token delta in `agent_loop.go`
6. Fix 6 — tool-call history reconstruction in `chatHistory`

## Files touched

| File | Fixes |
|---|---|
| `internal/server/generation.go` | 1, 2 |
| `internal/server/v2_handlers.go` | 3, 5 |
| `internal/server/agent_loop.go` | 4, 6 |
