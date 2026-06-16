# PLAN-v2-pass4.md

Fourth-pass fixes. All items confirmed by reading post-pass-3 source.

---

## Critical

### Fix 1 — `session.next.prompted` never emitted

**Files:** `internal/server/handlers.go:160`, `internal/server/v2_handlers.go:305`

Both prompt handlers only emit `session.next.prompt.admitted`. The protocol
requires `session.next.prompted` to be emitted first (before admission, to signal
the prompt was received), then `session.next.prompt.admitted` (after it enters
the queue/session). The TypeScript SDK and background-agents watch for
`session.next.prompted` to confirm delivery.

Fix: add `NewSessionNextPrompted` immediately before each `NewSessionNextPromptAdmitted`
call, using the same arguments:

In `handlers.go` (both async and sync paths) and `v2_handlers.go`:
```go
s.bus.Publish(event.NewSessionNextPrompted(sessionID, msgID, text, delivery))
s.bus.Publish(event.NewSessionNextPromptAdmitted(sessionID, msgID, text, delivery, seq))
```

`NewSessionNextPrompted` does not carry a seq (it precedes admission). The existing
constructor signature matches — verify in `event.go`.

### Fix 2 — SSE reconnect replay missing `session.updated`

**File:** `internal/server/v2_handlers.go:749`

The disk-replay block writes `message.updated` and `message.part.updated` per
message, plus `session.status{busy|idle}`. It does not write `session.updated`,
so a reconnecting client (or any new subscriber) never sees the session's current
title, cost, or token totals. The TUI session header stays blank.

Fix: emit `session.updated` as the first event in the replay block, before the
messages:

```go
if sess, ok := s.store.GetSession(sessionID); ok {
    s.writeEvent(w, flusher, event.NewSessionUpdated(sessionID, sess), event.KindEvent, "")
}
```

### Fix 3 — SSE reconnect replay missing pending `permission.asked`

**File:** `internal/server/v2_handlers.go:749`

If a permission request is pending when a client reconnects, the `permission.asked`
event is not replayed. The TUI reconnects, sees the session as busy, but has no
visible approval prompt — the user is stuck.

`permission.Store` already has `List()` returning all pending requests. After the
busy/idle status is written in the replay block, replay any pending permissions:

```go
for _, req := range s.perms.List() {
    if req.SessionID == sessionID {
        // Rebuild the same askObj shape emitted by the agent loop.
        askObj := map[string]any{
            "id":        req.ID,
            "sessionID": sessionID,
            "type":      req.Tool,
            "tool":      req.Tool,
            "pattern":   req.Pattern,
            "always":    []any{},
        }
        s.writeEvent(w, flusher, event.NewPermissionAsked(askObj), event.KindEvent, "")
    }
}
```

Check `permission.Request` struct for exact field names.

---

## Correctness

### Fix 4 — `reasoning` part missing from `mapToV2Message`

**File:** `internal/server/v2_handlers.go:498`

The `mapToV2Message` switch handles `text`, `tool`, `step-start`, `step-finish`
but not `"reasoning"`. Reasoning-model responses accumulate reasoning deltas as
`Part{Type: "reasoning", Text: "..."}` parts. These are silently dropped in v2
message responses.

Fix: add a case:
```go
case "reasoning":
    if p.Text == "" {
        continue
    }
    content = append(content, map[string]any{
        "type": "reasoning",
        "id":   p.ID,
        "text": p.Text,
    })
```

### Fix 5 — `model.id` missing from v2 assistant message

**File:** `internal/server/v2_handlers.go:531`

The assistant message shape uses:
```go
"model": map[string]any{"providerID": m.Info.ProviderID, "modelID": m.Info.ModelID}
```

The TUI model selector expects a combined `"id"` field (e.g. `"concactao/gpt-oss-120b-combo"`).
Add it:
```go
"model": map[string]any{
    "id":         m.Info.ProviderID + "/" + m.Info.ModelID,
    "providerID": m.Info.ProviderID,
    "modelID":    m.Info.ModelID,
}
```

Apply the same to the user message model block and the `modelV2Info` struct in
`handleV2ModelList`.

### Fix 6 — `handlePrompt` (sync) emits `session.status{busy}` before `runGenerationSync` which also registers a cancel

**File:** `internal/server/handlers.go:215`

`handlePrompt` calls `runGenerationSync` which (since pass 3) creates a
cancellable context via `registerCancel`. However `handlePrompt` emits busy
*before* the call and idle *after* — it bypasses the queue entirely.

The problem: if `POST /session/{id}/abort` fires between the busy emit and the
`registerCancel` inside `runGenerationSync`, there is no registered cancel yet
and the abort silently does nothing. The session stays busy until the turn
finishes naturally.

Fix: move the cancel registration one level up so it exists before the busy
event is emitted. Extract a `withCancel` helper:

```go
ctx, cancel := context.WithCancel(context.Background())
s.registerCancel(id, cancel)
defer func() { s.clearCancel(id); cancel() }()

s.bus.Publish(event.NewSessionStatus(id, map[string]string{"type": "busy"}))
asst, ok := s.runGenerationSyncCtx(ctx, id, ...)
```

Add `runGenerationSyncCtx(ctx context.Context, ...)` that accepts the context
instead of creating its own. `runGenerationSync` becomes a thin wrapper that
creates the context and calls `runGenerationSyncCtx`.

### Fix 7 — `processQueue` does not drain queued tasks after abort

**File:** `internal/server/session_handlers.go:142–155`

`handleSessionAbort` sets `work.queue = work.queue[:0]` under `sesMu`, then calls
`cancelSession`. After the current turn's context is cancelled, `runAgentLoop`
returns, `runGenerationSync` returns, and `processQueue` loops back and finds
the queue empty — emitting idle. This is the intended behavior (confirmed).

However there is a window: the abort drains the queue under `sesMu`, but
`processQueue` may have already dequeued the *next* task from the queue *before*
the abort acquired the lock:

```
abort: lock → drain queue → unlock
processQueue: lock → dequeue task → unlock  ← may happen BEFORE drain
```

If `processQueue` dequeued a second task before the drain, that task still runs.

Fix: add a `draining bool` flag to `sessionWork`. Set it in abort. In
`processQueue`, check the flag before starting each task:

```go
// in processQueue loop, after dequeuing:
if w.draining {
    w.draining = false
    w.running = false
    s.sesMu.Unlock()
    s.bus.Publish(event.NewSessionStatus(w.sessionID, map[string]string{"type": "idle"}))
    s.bus.Publish(event.NewSessionIdle(w.sessionID))
    return
}
```

---

## Missing features

### Fix 8 — MCP tools registered only at startup, not on connect/disconnect

**File:** `internal/server/mcp_handlers.go`

`server.go:76–77` registers MCP adapters at startup from the config file. If
`POST /mcp/{name}/connect` connects a new MCP server at runtime, its tools are
not added to `s.tools`. Similarly, disconnect does not remove them.

Fix: in `handleMCPConnect`, after the connection is established, register the
new server's tools:
```go
for _, t := range adapter.Tools() {
    s.tools.Register(t)
}
```

In `handleMCPDisconnect`, unregister them:
```go
s.tools.Unregister(name) // removes all tools with prefix "mcpServerName/"
```

Add `Unregister(prefix string)` to `tool.Registry` that removes all tools whose
`Name()` starts with the given prefix.

### Fix 9 — `GET /api/session/{id}/event` filter drops `session.updated` for other sessions

**File:** `internal/server/v2_handlers.go:655`

`eventBelongsToSession` returns true only when `eventSessionID(ev) == sessionID`.
`session.updated` events for the *current* session pass through, but there is no
case in `eventSessionID` for the case where Properties is `SessionUpdatedProps`
with a different session (already filtered). This is correct.

However: `event.NewSessionUpdated(sessionID, updated)` constructs the event with
`Properties: SessionUpdatedProps{SessionID: sessionID, ...}` — verify
`SessionUpdatedProps` has a `SessionID` field and that it's populated. If
`NewSessionUpdated` wraps the raw `Session` struct (which has no `sessionID`
field, just `ID`), `eventSessionID` falls through to `return ""` and the event
is dropped from all per-session streams.

Read `event.go:NewSessionUpdated` and `SessionUpdatedProps` to confirm. If the
session ID is not embedded in Properties, either:
- Add `SessionID` to `SessionUpdatedProps`, or
- Add a special case to `eventSessionID` for `session.Session` typed properties:
  ```go
  case session.Session:
      return p.ID
  ```

### Fix 10 — No `POST /log` endpoint

**File:** `internal/server/router.go`

The TUI posts structured log entries to `POST /log`. This is referenced in
`boot_conformance_test.go`. Currently missing from the router.

Implementation: accept and silently discard (the server has its own logger):
```go
mux.HandleFunc("POST /log", func(w http.ResponseWriter, r *http.Request) {
    io.Copy(io.Discard, r.Body)
    writeJSON(w, http.StatusOK, true)
})
```

Or forward to the server logger at DEBUG level if the body is valid JSON.

---

## Implementation order

1. Fix 1 — emit `session.next.prompted` (one-liner, high impact)
2. Fix 2 — `session.updated` in SSE replay (one-liner)
3. Fix 3 — pending permissions in SSE replay
4. Fix 4 — `reasoning` part in `mapToV2Message`
5. Fix 5 — `model.id` in v2 message/model responses
6. Fix 6 — cancel registration before busy emit in sync path
7. Fix 7 — draining flag in processQueue
8. Fix 9 — verify `session.updated` passes through per-session stream
9. Fix 10 — `POST /log` endpoint
10. Fix 8 — MCP runtime tool registration (most complex, last)

## Files touched

| File | Fixes |
|---|---|
| `internal/server/handlers.go` | 1, 6 |
| `internal/server/v2_handlers.go` | 1, 2, 3, 4, 5, 9 |
| `internal/server/generation.go` | 6 |
| `internal/server/session_handlers.go` | 7 |
| `internal/server/router.go` | 10 |
| `internal/server/mcp_handlers.go` | 8 |
| `internal/tool/tool.go` | 8 (Unregister) |
| `internal/event/event.go` | 9 (verify SessionUpdatedProps.SessionID) |
