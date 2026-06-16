# PLAN-v2-review-fixes.md

Fix all bugs and correctness issues found in the v2 parity code review.

---

## Fix 1 — nil-guard tool parts in `mapToV2Message`

**File:** `internal/server/v2_handlers.go`  
**Severity:** BUG (latent panic)

`p.State.Status` is accessed unconditionally for `"tool"` type parts. Add a nil check:

```go
case "tool":
    if p.State == nil {
        continue
    }
    state := map[string]any{ ... }
```

---

## Fix 2 — images never reach the model

**File:** `internal/server/agent_loop.go:52`  
**Severity:** BUG (images silently dropped)

`chatHistory` checks `i == len(msgs)-1` to find the last user message and attach
images, but `runAgentLoop` is called after `NewAssistantMessage` has already appended
an assistant entry to the store. The last entry is now the in-progress assistant
message, so the condition is never true for any user message.

Fix: compare by message ID instead of by position.

The caller already knows the current user message ID (`userMsgID` in
`runGenerationSync`). Pass it through to `chatHistory` and match on ID:

```go
func (s *Server) chatHistory(sessionID, currentUserMsgID string, currentTexts []string, currentImages []string) []provider.ChatMessage
```

Inside the loop:
```go
if role == "user" && msg.Info.ID == currentUserMsgID {
    content = provider.MultimodalContent(text, currentImages)
}
```

Update `runAgentLoop` signature and all call sites to pass `userMsgID`.

---

## Fix 3 — TOCTOU race in steer-conflict check

**File:** `internal/server/v2_handlers.go:178`  
**Severity:** BUG (two steer prompts can race through)

The check reads `work.running` under `sesMu`, releases the lock, then falls through
to `startOrQueue` which re-acquires it. Between the two lock acquisitions a concurrent
request can start a turn.

Fix: hold the lock across both the check and the initial state mutation. The simplest
approach is to push the conflict check inside `startOrQueue` (which already holds
`sesMu`), returning a boolean indicating whether the prompt was rejected:

```go
// startOrQueue returns false if delivery=="steer" and the session is busy.
func (s *Server) startOrQueue(..., delivery string) bool {
    s.sesMu.Lock()
    w := s.sesQueue[sessionID]
    if w != nil && w.running && delivery == "steer" {
        s.sesMu.Unlock()
        return false
    }
    // ... existing queue/run logic
    return true
}
```

In `handleV2SessionPrompt`:
```go
if !s.startOrQueue(sessionID, ..., delivery) {
    writeJSON(w, http.StatusConflict, conflictError)
    return
}
```

Remove the separate pre-check block in the handler.

---

## Fix 4 — pagination cursor always null

**File:** `internal/server/v2_handlers.go:71`  
**Severity:** WRONG

The cursor is hardcoded to `{"previous": null, "next": null}`. Clients see a
truncated list with no signal that more pages exist.

Fix: implement offset-based cursor encoded as base64(strconv.Itoa(offset)):

```go
sessions := s.store.List() // all sessions, sorted desc by updated time
total := len(sessions)
offset := 0
if cur := r.URL.Query().Get("cursor"); cur != "" {
    if b, err := base64.StdEncoding.DecodeString(cur); err == nil {
        offset, _ = strconv.Atoi(string(b))
    }
}
end := min(offset+limit, total)
page := sessions[offset:end]

var nextCursor, prevCursor any
if end < total {
    nextCursor = base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(end)))
}
if offset > 0 {
    prev := max(0, offset-limit)
    prevCursor = base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(prev)))
}
writeJSON(w, http.StatusOK, map[string]any{
    "data":   page,
    "cursor": map[string]any{"previous": prevCursor, "next": nextCursor},
})
```

Apply the same pattern to `handleV2SessionMessages`.

---

## Fix 5 — user message model is always `{"providerID":"","modelID":""}`

**File:** `internal/server/v2_handlers.go:368`  
**Severity:** WRONG

User messages store their model in `Info.Model` (a `*MsgModel` pointer), not in
`Info.ProviderID`/`Info.ModelID` (those are assistant-only fields).

Fix in `mapToV2Message`:
```go
// user branch
providerID := ""
modelID := ""
if m.Info.Model != nil {
    providerID = m.Info.Model.ProviderID
    modelID    = m.Info.Model.ModelID
}
"model": map[string]any{"providerID": providerID, "modelID": modelID},
```

---

## Fix 6 — `admitSeq` must be per-session

**File:** `internal/server/server.go`, `internal/server/v2_handlers.go`  
**Severity:** WRONG

`s.admitSeq` is a single global `atomic.Uint64` shared across all sessions. The
protocol expects a per-session monotonically-increasing counter.

Fix: replace the global counter with a per-session map protected by `sesMu`
(already held during prompt admission):

```go
// in Server struct — remove admitSeq atomic.Uint64
sesAdmitSeq map[string]int64   // protected by sesMu
```

In `startOrQueue` (after Fix 3 merges the check here), increment and return:
```go
w.admitSeq++
seq := w.admitSeq
```

Pass `seq` back to the handler through a return value or a dedicated call, and
include it in `SessionInputAdmitted.AdmittedSeq`.

---

## Fix 7 — auto-title in `generation.go` is dead code

**File:** `internal/server/generation.go:248`  
**Severity:** WRONG

`ensureSessionTitle` checks `sess.Title == ""` before acting. But `AppendUserMessage`
in the store already calls `sessionTitleFromTexts` and sets the title at the point
the user message is appended (store.go:295). By the time `finishGeneration` calls
`ensureSessionTitle`, the title is already non-empty, so the function returns
immediately and `session.updated` is never published for new titles.

Fix: Remove `ensureSessionTitle` from `finishGeneration`. The store-level auto-title
is the authoritative path. After `AppendUserMessage` sets the title, publish
`session.updated` from within the store (or from the handler immediately after the
`AppendUserMessage` call) so the TUI updates its session list.

In `handlers.go` / `session_handlers.go`, after `AppendUserMessage`:
```go
if updated, ok := s.store.GetSession(sessionID); ok {
    s.bus.Publish(event.NewSessionUpdated(updated))
}
```

Delete `ensureSessionTitle` and its call site in `generation.go`.

---

## Fix 8 — summarize must force-update title

**File:** `internal/server/session_handlers.go:44`  
**Severity:** WRONG

`handleSessionSummarize` calls `ensureSessionTitle` which is a no-op when the
title is already set. A session with a stale/wrong title can never be corrected.

After Fix 7 removes `ensureSessionTitle`, replace the summarize handler with a
force-update: re-derive the title from the first user message text unconditionally
and publish `session.updated`.

```go
func (s *Server) handleSessionSummarize(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if _, ok := s.store.GetSession(id); !ok {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }
    if msgs, ok := s.store.Messages(id); ok {
        for _, m := range msgs {
            if m.Info.Role == "user" {
                title := firstLine(partsText(m.Parts, "text"), 60)
                if title != "" {
                    s.store.UpdateSessionTitle(id, title)
                    if updated, ok := s.store.GetSession(id); ok {
                        s.bus.Publish(event.NewSessionUpdated(updated))
                    }
                }
                break
            }
        }
    }
    writeJSON(w, http.StatusOK, true)
}
```

---

## Fix 9 — `/wait` missed-event window

**File:** `internal/server/v2_handlers.go:253`  
**Severity:** WRONG

Current order: acquire lock → check running → release lock → subscribe to bus.
If the session goes idle in the gap between unlock and subscribe, the `session.idle`
event is missed and the caller stalls for 60 seconds.

Fix: subscribe first, then check under the lock, and cancel the subscription if
already idle:

```go
sub, cancel := s.bus.Subscribe()
defer cancel()

s.sesMu.Lock()
work := s.sesQueue[sessionID]
idle := work == nil || !work.running
s.sesMu.Unlock()

if idle {
    w.WriteHeader(http.StatusNoContent)
    return
}
// ... event loop
```

---

## Fix 10 — `session.deleted` invisible on per-session stream

**File:** `internal/server/v2_handlers.go:655`  
**Severity:** WRONG

`eventSessionID` has no case for `event.SessionDeletedProps`. It falls through to
`return ""`, causing `session.deleted` to be filtered out of the per-session stream.

`SessionDeletedProps` carries the full `session.Session` value (not a sessionID
string). Add a case:

```go
case event.SessionDeletedProps:
    return p.Session.ID  // or however SessionDeletedProps exposes the ID
```

Check the actual `SessionDeletedProps` struct in `event.go` to confirm the field name.

---

## Fix 11 — `admittedSeq` missing from SSE event

**File:** `internal/event/event.go`  
**Severity:** MISSING

`SessionNextPromptProps` has no `admittedSeq` field. The TypeScript SDK uses this
field on the `session.next.prompt.admitted` event to correlate prompts to their
streaming events.

Add the field:
```go
type SessionNextPromptProps struct {
    Timestamp   int64  `json:"timestamp"`
    SessionID   string `json:"sessionID"`
    MessageID   string `json:"messageID"`
    AdmittedSeq int64  `json:"admittedSeq"`
    Prompt      struct {
        Text string `json:"text"`
    } `json:"prompt"`
    Delivery string `json:"delivery"`
}
```

Update `NewSessionNextPromptAdmitted` to accept and embed the seq. Update the call
site in `startOrQueue` (after Fix 6 moves the counter there) to pass the seq.

---

## Fix 12 — replay on per-session event stream not session-scoped

**File:** `internal/server/v2_handlers.go:604`, `internal/event/bus.go:55`  
**Severity:** MISSING

`bus.Subscribe()` replays all `b.recent` events into the new subscriber's channel
before returning. For a per-session stream, this dumps events from every session
into a 256-element channel. On a busy server, the channel fills with irrelevant
events and session-specific replay events are silently dropped (the channel write
is non-blocking in `Subscribe`).

Fix: add a filtered-replay helper to the bus:

```go
// SubscribeFiltered subscribes and replays only events matching the predicate.
func (b *Bus) SubscribeFiltered(match func(Event) bool) (*Subscriber, func()) {
    b.mu.Lock()
    b.seq++
    id := b.seq
    s := &Subscriber{id: id, ch: make(chan Event, subBufSize), done: make(chan struct{})}
    for _, ev := range b.recent {
        if match(ev) {
            select {
            case s.ch <- ev:
            default: // drop if full — subscriber is new, shouldn't happen in practice
            }
        }
    }
    b.subs[id] = s
    b.mu.Unlock()
    cancel := func() { /* same as Subscribe */ }
    return s, cancel
}
```

In `handleV2SessionEvent`:
```go
sub, cancel := s.bus.SubscribeFiltered(func(ev event.Event) bool {
    return eventBelongsToSession(ev, sessionID)
})
```

---

## Fix 13 — `webFetchTool` swallows non-2xx status

**File:** `internal/tool/builtins_readonly.go`  
**Severity:** MISSING

On a 4xx/5xx response the body is returned as-is with no status indication. The
model cannot distinguish a successful 200 from a 404 error page.

Fix: prefix the output with the status line when status is not 2xx:

```go
if resp.StatusCode < 200 || resp.StatusCode >= 300 {
    text = fmt.Sprintf("HTTP %d %s\n\n%s", resp.StatusCode, resp.Status, text)
}
```

---

## Fix 14 — `handleV2Location` ignores `s.workdir`

**File:** `internal/server/v2_handlers.go:23`  
**Severity:** MINOR

When the `?directory=` query param is absent, `directoryParam(r)` returns `""` and
`filepath.Base("")` returns `"."`. The actual working directory is lost.

Fix: fall back to `s.workdir`:
```go
dir := directoryParam(r)
if dir == "" {
    dir = s.workdir
}
```

---

## Fix 15 — no conflict-check on client-supplied session ID

**File:** `internal/server/v2_handlers.go:112`  
**Severity:** MINOR

`CreateSessionWithID` with a non-empty ID that duplicates an existing session
silently overwrites it.

Fix: in `handleV2SessionCreate`, when `req.ID != ""`, check for an existing session:
```go
if req.ID != "" {
    if _, exists := s.store.GetSession(req.ID); exists {
        writeJSON(w, http.StatusConflict, map[string]any{
            "_tag": "ConflictError", "message": "session already exists", "resource": "session",
        })
        return
    }
}
```

---

## Fix 16 — `Cost.Cache.Write` typed as `int64`

**File:** `internal/server/v2_handlers.go:460`  
**Severity:** MINOR

`modelV2Info.Cost.Cache.Write` is `int64` while all other cost fields are `float64`.
Change to `float64` for consistency.

---

## Implementation order

Fix these in dependency order to avoid cascading breakage:

1. **Fix 7** — remove dead `ensureSessionTitle` from `finishGeneration` (clears confusion before touching title elsewhere)
2. **Fix 8** — rewrite `handleSessionSummarize` with force-update
3. **Fix 3 + Fix 6** — merge steer-conflict check into `startOrQueue`, add per-session admit seq (these touch the same function)
4. **Fix 11** — add `admittedSeq` to `SessionNextPromptProps` and wire through (depends on Fix 6 having the seq)
5. **Fix 2** — fix `chatHistory` image attachment (pass `currentUserMsgID`, update all call sites)
6. **Fix 1** — nil-guard tool parts in `mapToV2Message`
7. **Fix 5** — user message model from `Info.Model`
8. **Fix 9** — subscribe-before-check in `/wait`
9. **Fix 10** — `session.deleted` in `eventSessionID`
10. **Fix 12** — `SubscribeFiltered` on bus + use in `handleV2SessionEvent`
11. **Fix 4** — real pagination cursor (session list + message list)
12. **Fix 13** — non-2xx status prefix in `webFetchTool`
13. **Fix 14, 15, 16** — minors (location workdir fallback, ID conflict check, type fix)

## Files touched

| File | Fixes |
|---|---|
| `internal/server/v2_handlers.go` | 1, 3, 4, 5, 9, 10, 14, 15 |
| `internal/server/agent_loop.go` | 2 |
| `internal/server/generation.go` | 7 |
| `internal/server/session_handlers.go` | 8 |
| `internal/server/server.go` | 6 (remove admitSeq field) |
| `internal/server/generation.go` | 3, 6 (startOrQueue changes) |
| `internal/event/event.go` | 11 |
| `internal/event/bus.go` | 12 |
| `internal/tool/builtins_readonly.go` | 13 |
| `internal/server/handlers.go` | 7 (publish session.updated after AppendUserMessage) |
