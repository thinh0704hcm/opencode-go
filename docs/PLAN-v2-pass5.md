# PLAN-v2-pass5.md

Fifth-pass fixes. All items confirmed by reading post-pass-4 source and
cross-checking against `@opencode-ai/sdk` v2 TypeScript types.

---

## Critical

### Fix 1 — `sessionV2Info` missing required `projectID` field

**File:** `internal/server/v2_handlers.go:45`

`sessionV2Info` struct and `mapToV2Info` don't emit `projectID`. SDK type
`SessionV2Info` requires `projectID: string`. The TUI groups sessions by
project — missing field causes silent parse failures or display gaps.

Fix: derive `projectID` from `filepath.Base(sess.Directory)` (same logic as
`/path` and `/project/current`). Add to struct and populate in `mapToV2Info`:

```go
ProjectID string `json:"projectID"`
```
```go
info.ProjectID = filepath.Base(sess.Directory)
if info.ProjectID == "." || info.ProjectID == "" {
    info.ProjectID = "global"
}
```

Also add optional `Agent` and `Model` fields to `sessionV2Info` and populate
from the last assistant message in `mapToV2Info`:

```go
Agent string `json:"agent,omitempty"`
Model *struct {
    ID         string `json:"id"`
    ProviderID string `json:"providerID"`
} `json:"model,omitempty"`
```

### Fix 2 — `mapToV2Message` tool state missing `title`, `metadata`, `time`

**File:** `internal/server/v2_handlers.go:522`

The `ToolStateCompleted` SDK type requires `title: string`, `metadata: {}`,
and `time: {start, end}`. Our mapper only emits `{status, input, output}`.
TUI tool-result display reads `state.title` and `state.time` — both missing.

Fix: update the tool case in `mapToV2Message`:

```go
state := map[string]any{
    "status":   p.State.Status,
    "input":    p.State.Input,
    "output":   p.State.Output,
    "title":    p.State.Title,
    "metadata": p.State.Metadata,
}
if p.State.Time != nil {
    state["time"] = map[string]any{
        "start": p.State.Time.Start,
        "end":   p.State.Time.End,
    }
}
```

For `"error"` status, also map `output` → `error` field (SDK uses `error`
instead of `output`):

```go
if p.State.Status == "error" {
    state["error"] = p.State.Output
    delete(state, "output")
}
```

### Fix 3 — `handleV2SessionWait` missed-event window

**File:** `internal/server/v2_handlers.go:319`

Current code: (1) subscribe, (2) lock+check work.running, (3) unlock, (4) wait.
But between (1) and (3) the session can become idle — the idle event was
emitted between Subscribe and the lock, so it's in the ring buffer.

Fix: subscribe BEFORE reading the running state, then check the ring buffer
replay. The current implementation actually subscribes first (line 326), THEN
reads `work.running` (line 329). This is backwards from the comment in the
pass-2 plan but actually correct in code.

However there's a real race: the Subscribe() call replays recent events
AFTER returning, and the idle check AFTER unlock means an idle emitted between
the sub start and the running check won't appear in the channel. Fix by using
SubscribeFiltered with replay so the idle event is captured:

```go
sub, cancel := s.bus.SubscribeFiltered(func(ev event.Event) bool {
    return ev.Type == event.TypeSessionIdle && eventSessionID(ev) == sessionID
})
defer cancel()

s.sesMu.Lock()
work := s.sesQueue[sessionID]
idle := work == nil || !work.running
s.sesMu.Unlock()

if idle {
    // Also drain pending replay — idle may have arrived just before subscribe
    select {
    case <-sub.Events():
    default:
    }
    w.WriteHeader(http.StatusNoContent)
    return
}
```

---

## Correctness

### Fix 4 — `WebFetch` uses `http.DefaultClient` with no timeout

**File:** `internal/tool/builtins_readonly.go:221`

`http.DefaultClient.Do(req)` has no timeout. A slow or hung HTTP server will
block the agent loop forever, preventing abort from working.

Fix: use a timeout-bounded client:

```go
client := &http.Client{Timeout: 30 * time.Second}
resp, err := client.Do(req)
```

The existing ctx from `Execute` already handles abort — the `req` carries
`ctx`. Keep the per-request context AND add the 30s wall-clock safety net.

### Fix 5 — Abort idle session emits no feedback event

**File:** `internal/server/session_handlers.go:147`

When `POST /session/{id}/abort` is called on an already-idle session (no
work entry in `sesQueue`), the handler just returns `200 true`. The TUI
may be watching the SSE stream for an idle event as confirmation.

Fix: emit idle when no work is running:

```go
s.sesMu.Lock()
work := s.sesQueue[id]
if work == nil || !work.running {
    s.sesMu.Unlock()
    s.bus.Publish(event.NewSessionStatus(id, map[string]string{"type": "idle"}))
    s.bus.Publish(event.NewSessionIdle(id))
    writeJSON(w, http.StatusOK, true)
    return
}
// ... existing drain+cancel path
```

### Fix 6 — `handleV2SessionPrompt` `prompt` field vs `parts` inconsistency

**File:** `internal/server/v2_handlers.go:218`

The v1 SDK sends `{parts: [{type:"text", text:"..."}]}` via `prompt_async`.
The v2 SDK sends `{prompt: {text: "..."}, delivery: "queue"|"steer"}`.
Our `v2PromptRequest` struct has `Prompt struct{Text string}` but also
assembles texts from `parts` — check that both paths work:

In `handleV2SessionPrompt`, `texts := []string{req.Prompt.Text}`. If the
TUI sends both `prompt.text` and `parts`, only `prompt.text` is used.
This is correct for v2 protocol. No change needed — document it as verified.

### Fix 7 — `mapToV2Message` `step-start`/`step-finish` missing `snapshot`/`reason`

**File:** `internal/server/v2_handlers.go:536`

SDK types:
- `StepStartPart: {id, sessionID, messageID, type: "step-start", snapshot?: string}`
- `StepFinishPart: {id, sessionID, messageID, type: "step-finish", reason: string, cost, tokens}`

Our mapper emits `{type, id}` for both — missing `snapshot`, `reason`, `cost`,
`tokens`. `reason` is in `p.Finish`; `cost`/`tokens` are in the step-finish
part's metadata.

Fix: enrich step-start and step-finish parts:

```go
case "step-start":
    content = append(content, map[string]any{
        "type": "step-start",
        "id":   p.ID,
    })
case "step-finish":
    sf := map[string]any{
        "type":   "step-finish",
        "id":     p.ID,
        "reason": p.Finish,
        "cost":   p.Cost,
    }
    if p.Tokens != nil {
        sf["tokens"] = p.Tokens
    }
    content = append(content, sf)
```

---

## Missing features

### Fix 8 — `GET /api/session/{sessionID}/event` missing `session.updated` on replay

**File:** `internal/server/v2_handlers.go:768`

The per-session SSE replay already emits `session.updated`. Verified correct.
But it doesn't emit `session.next.prompt.admitted` for any pending but
not-yet-started items in the queue.

Fix: after the busy/idle block, if `work != nil && work.running`, also
replay queued item count as a hint. Not critical for TUI but good for
background-agent polling clients. Defer to pass 6.

### Fix 9 — `sessionV2Info` `time.updated` not refreshed after generation

**File:** `internal/session/store.go`

After a generation turn, `session.Time.Updated` should be bumped. Currently
`PersistSession` saves to disk but doesn't bump `Time.Updated`. The TUI sorts
sessions by last-updated — stale timestamps make the session appear old.

Fix: in `CompleteAssistantMessage`, bump the parent session's `Time.Updated`:

```go
func (s *Store) CompleteAssistantMessage(sessionID, messageID string) (MessageInfo, bool) {
    // ... existing logic ...
    // Bump session updated time
    if sess := s.sessions[sessionID]; sess != nil {
        sess.Time.Updated = now
    }
    return ...
}
```

---

## Implementation order

1. Fix 1 — `projectID` + `agent`/`model` in `sessionV2Info`
2. Fix 2 — tool state `title`/`metadata`/`time` in v2 mapper
3. Fix 4 — WebFetch 30s timeout
4. Fix 5 — abort idle fallback idle event
5. Fix 3 — `/wait` `SubscribeFiltered` (narrowed subscription)
6. Fix 7 — step-finish `reason`/`cost`/`tokens`
7. Fix 9 — session `time.updated` after generation

## Files touched

| File | Fixes |
|---|---|
| `internal/server/v2_handlers.go` | 1, 2, 3, 7 |
| `internal/server/session_handlers.go` | 5 |
| `internal/tool/builtins_readonly.go` | 4 |
| `internal/session/store.go` | 9 |
