# PLAN-v2-pass3.md

Third-pass fixes and feature completions. All items confirmed by reading the
post-pass-2 source.

---

## Critical bugs

### Fix 1 — Abort is completely broken

**File:** `internal/server/generation.go:42`

`runAgentLoop` is called with `context.Background()`. The `registerCancel` /
`cancelSession` machinery exists in `server.go` but is never wired into the new
`processQueue` → `runGenerationSync` → `runAgentLoop` pipeline. `POST
/session/{id}/abort` calls `cancelSession`, which fires a no-op cancel func (or
nothing if never registered).

Fix: `runGenerationSync` must create a cancellable context, register it, and
clear it on exit:

```go
func (s *Server) runGenerationSync(...) (session.MessageWithParts, bool) {
    ctx, cancel := context.WithCancel(context.Background())
    s.registerCancel(sessionID, cancel)
    defer func() {
        s.clearCancel(sessionID)
        cancel()
    }()

    // ...
    s.runAgentLoop(ctx, sessionID, ...)
    // ...
}
```

### Fix 2 — `startOrQueue` called before `AppendUserMessage` (race)

**File:** `internal/server/v2_handlers.go:262–272`

In `handleV2SessionPrompt`:
```go
seq, ok := s.startOrQueue(...)   // goroutine may start immediately
// ...
msg, ok := s.store.AppendUserMessage(...)  // user message written AFTER
```

If the goroutine starts immediately (idle session), it calls `chatHistory`
before the user message exists in the store. The new message is silently
omitted from the history, so the model sees no user prompt for this turn.

Fix: call `AppendUserMessage` (and `publishUserMessage`) **before**
`startOrQueue`. The v1 `handlePromptAsync` already does this correctly — match
that order:

```go
msg, ok := s.store.AppendUserMessage(sessionID, msgID, ...)
if !ok { ... }
s.publishUserMessage(sessionID, msg)

seq, ok := s.startOrQueue(sessionID, msgID, ..., delivery)
if !ok { ... conflict response ... }
```

---

## Correctness issues

### Fix 3 — `session.status{busy}` double-emitted

**File:** `internal/server/generation.go:119`

`processQueue` emits `session.status{busy}` before calling `runGenerationSync`.
The old `handlePromptAsync` / `handlePrompt` path no longer emits busy (that
was in the pre-refactor `runGenerationSync`). Verify by grepping that only
`processQueue` emits busy — if the old path still calls `NewSessionStatus{busy}`
independently, remove the duplicate. If `processQueue` is the sole emitter,
this is fine.

**Also:** `processQueue` emits `session.status{idle}` + `session.idle` after
the queue drains, but `handleSessionAbort` also emits these. After Fix 1 lands,
aborting will cancel the context; `runGenerationSync` returns; `processQueue`
continues its loop, finds the queue empty, and emits idle — correct. But abort
handler also emits idle. Deduplicate: abort should emit idle only when the
session was actually running (i.e., `cancelSession` returned true).

### Fix 4 — `v2PromptRequest` ignores model and agent

**File:** `internal/server/v2_handlers.go:197`

`v2PromptRequest` has no `Model` or `Agent` fields. The TypeScript SDK sends:
```json
{ "prompt": {"text": "..."}, "model": {"providerID": "...", "modelID": "..."}, "agent": "coder" }
```

The handler always uses `s.provider.ID()` / `s.model` / default agent.

Fix: add fields to the request struct and use them:
```go
type v2PromptRequest struct {
    ID     string `json:"id"`
    Prompt struct { ... } `json:"prompt"`
    Model  *struct {
        ProviderID string `json:"providerID"`
        ModelID    string `json:"modelID"`
    } `json:"model"`
    Agent    string `json:"agent"`
    Delivery string `json:"delivery"`
    Resume   bool   `json:"resume"`
}
```

In the handler:
```go
providerID := s.provider.ID()
modelID := s.model
if req.Model != nil && req.Model.ModelID != "" {
    providerID = req.Model.ProviderID
    modelID = req.Model.ModelID
}
agent, _ := resolveAgent(s.workdir, req.Agent)
```

### Fix 5 — `session.next.prompted` events emitted inside `startOrQueue` before user message is persisted

**File:** `internal/server/generation.go:startOrQueue`

`startOrQueue` emits `session.next.prompted` and `session.next.prompt.admitted`
with the `texts` slice it receives. But after Fix 2 reorders the calls, the
user message is in the store before `startOrQueue` runs, so this is fine.
Verify after Fix 2 that the event `messageID` and `text` match what's stored.

### Fix 6 — `runAgentLoop` context passed from processQueue is not abortable from handler

Related to Fix 1. After Fix 1, `runGenerationSync` creates the cancellable
context. However, `processQueue` calls `runGenerationSync` in a loop — each
call re-registers a new cancel for the same session. When the queue has multiple
items, cancelling mid-turn cancels only the current task's context; the next
queued task starts with a fresh context. This is correct behavior (abort cancels
the current turn, not the whole queue). Document this explicitly.

Also: after an abort, the queue still has pending items. Decide policy: should
abort drain the entire queue or only the current turn? For now, drain the queue
on abort (set `w.queue = nil` under `sesMu` before cancelling) so the session
goes fully idle.

```go
func (s *Server) handleSessionAbort(...) {
    id := r.PathValue("id")
    // Drain queue first so no more items start after the cancel.
    s.sesMu.Lock()
    if w := s.sesQueue[id]; w != nil {
        w.queue = w.queue[:0]
    }
    s.sesMu.Unlock()
    s.cancelSession(id)
    // idle events emitted by processQueue when it sees empty queue after cancel
    writeJSON(w, http.StatusOK, true)
}
```

Remove the manual idle event publish from `handleSessionAbort` — `processQueue`
will emit them naturally when the queue is empty.

---

## Missing features

### Fix 7 — Session revert / unrevert

**Routes:** `POST /session/{id}/revert`, `POST /session/{id}/unrevert`
**Current:** both are `handleSessionNoop`

`revert` should stash uncommitted changes for the session's working directory:
```go
func (s *Server) handleSessionRevert(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    sess, ok := s.store.GetSession(id)
    if !ok { writeError(w, 404, "session not found"); return }
    dir := sess.Directory
    if dir == "" { dir = s.workdir }
    out, err := exec.CommandContext(r.Context(), "git", "-C", dir, "stash", "push", "-u", "--message", "opencode-revert-"+id).CombinedOutput()
    if err != nil {
        writeError(w, 500, strings.TrimSpace(string(out)))
        return
    }
    writeJSON(w, 200, true)
}
```

`unrevert` pops the stash:
```go
exec.CommandContext(r.Context(), "git", "-C", dir, "stash", "pop")
```

Register both in `router.go` replacing the noop handlers.

### Fix 8 — Session fork copies messages from parent

**File:** `internal/server/session_handlers.go:handleSessionFork`

Currently creates an empty child session. A real fork should duplicate the
parent's messages so the child starts with the same conversation context.

```go
func (s *Server) handleSessionFork(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    parent, ok := s.store.GetSession(id)
    if !ok { writeError(w, 404, "session not found"); return }

    child := s.store.CreateSession(id, parent.Title+" (fork)", parent.Directory)
    // Copy messages from parent into child
    if msgs, ok := s.store.Messages(id); ok {
        for _, m := range msgs {
            s.store.CopyMessage(child.ID, m)
        }
    }
    s.store.PersistSession(child.ID)
    writeJSON(w, 200, child)
}
```

Add `CopyMessage(targetSessionID string, m session.MessageWithParts)` to the
store. It deep-copies the `MessageInfo` and `Parts` slice with a new message ID
to avoid ID collisions.

### Fix 9 — MCP tools not exposed to the agent

**File:** `internal/server/agent_loop.go`, `internal/server/tool_schemas.go`

`toolSchemas(s.tools, agent.toolAllowed)` only iterates the server's built-in
tool registry. MCP servers connected via `s.mcp` expose their own tools via
the adapter, but these are never included in `req.Tools`.

Fix: expose MCP tools through the same `tool.Registry` interface. After an MCP
server connects, register its tools in `s.tools` via the existing
`RegisterTool` / `tool.Registry` API.

In `handleMCPConnect` (or the MCP adapter's `OnToolsChanged` callback), call:
```go
for _, t := range mcpTools {
    s.tools.Register(mcpToolAdapter{server: name, tool: t})
}
```

`mcpToolAdapter` wraps the MCP client call behind the `tool.Tool` interface
(`Name()`, `Mutating()`, `Execute()`).

### Fix 10 — Per-session SSE replay does not include disk-persisted history

**File:** `internal/server/v2_handlers.go:handleV2SessionEvent`

`SubscribeFiltered` replays only the in-memory `b.recent` ring buffer (last
2048 events). For a freshly-started server or a long-running session whose
events rolled out of the buffer, a reconnecting client gets no replay.

For `session.idle`, `message.updated` (guaranteed-delivery), and
`session.status`, load the current state from the store and synthesise replay
events directly before subscribing:

```go
// Synthesise state-restore events from persisted store before joining live stream.
if msgs, ok := s.store.Messages(sessionID); ok {
    for _, m := range msgs {
        w.Write(sseEvent(event.NewMessageUpdated(sessionID, m.Info, m.Info.Time.Completed != nil)))
        for _, p := range m.Parts {
            w.Write(sseEvent(event.NewMessagePartUpdated(sessionID, p, p.Time)))
        }
    }
}
// Then subscribe to live events.
```

This ensures a reconnecting client sees the full conversation even after a
server restart.

---

## Minor

### Fix 11 — `handleV2SessionList` default sort order

The store's `List()` returns sessions in insertion order (ascending). The v2
API convention is newest-first (descending). Reverse the slice before applying
the cursor when `order == ""` (i.e., when the client omits the param, default
to `"desc"`):

```go
order := r.URL.Query().Get("order")
if order == "" {
    order = "desc"
}
```

### Fix 12 — `POST /tui/publish` should forward to event bus

**File:** `internal/server/router.go:124`

`POST /tui/publish` is wired to `handleTUIOK` (returns 200 `true`). The TUI
uses this endpoint to inject custom events into the bus (e.g. triggering a
toast notification or custom UI action). It should decode the body as an event
and publish it:

```go
func (s *Server) handleTUIPublish(w http.ResponseWriter, r *http.Request) {
    var ev event.Event
    if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
        writeError(w, 400, "invalid event")
        return
    }
    s.bus.Publish(ev)
    writeJSON(w, 200, true)
}
```

### Fix 13 — `handleV2SessionMessages` skips `step-start` / `step-finish` parts

**File:** `internal/server/v2_handlers.go:mapToV2Message`

The content array only includes `"text"` and `"tool"` typed parts. Parts with
type `"step-start"` and `"step-finish"` are silently dropped. The v2 SDK may
use these to determine loading state on reconnect.

Add cases:
```go
case "step-start", "step-finish":
    content = append(content, map[string]any{
        "type": p.Type,
        "id":   p.ID,
    })
```

---

## Implementation order

1. Fix 1 — wire cancellable context through `runGenerationSync` (unblocks abort)
2. Fix 6 — drain queue on abort in `handleSessionAbort`
3. Fix 2 — reorder `AppendUserMessage` before `startOrQueue`
4. Fix 3 — verify/deduplicate busy event and idle event from abort vs processQueue
5. Fix 4 — add model/agent fields to `v2PromptRequest`
6. Fix 7 — session revert/unrevert (git stash)
7. Fix 8 — session fork copies messages (needs `store.CopyMessage`)
8. Fix 9 — MCP tools in agent schema
9. Fix 10 — disk-replay on per-session SSE reconnect
10. Fix 11 — default sort order desc
11. Fix 12 — `POST /tui/publish` forwards to bus
12. Fix 13 — step-start/step-finish in v2 message content

## Files touched

| File | Fixes |
|---|---|
| `internal/server/generation.go` | 1, 2, 3 |
| `internal/server/session_handlers.go` | 6, 7, 8 |
| `internal/server/v2_handlers.go` | 2, 4, 10, 11, 13 |
| `internal/server/server.go` | 6 (abort drains queue) |
| `internal/server/agent_loop.go` | 1 (ctx flows through) |
| `internal/server/router.go` | 7, 12 |
| `internal/server/mcp_handlers.go` | 9 |
| `internal/tool/` | 9 (mcpToolAdapter) |
| `internal/session/store.go` | 8 (CopyMessage) |
