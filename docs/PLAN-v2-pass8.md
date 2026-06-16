# PLAN-v2-pass8.md

Eighth-pass fixes. All items confirmed against post-pass-7 source and the
complete `@opencode-ai/sdk` v2 route list (24 routes in sdk.gen.js).

---

## Audit: pass 7 confirmed complete

All six pass-7 items verified in current code:

| Item | File:line | Status |
|---|---|---|
| `AppendStepFinish` called | `generation.go:79` | ✅ |
| Initial step-start part published | `generation.go:53-55` | ✅ |
| Text part `time`/`metadata` | `v2_handlers.go:532-538` | ✅ |
| Per-step token deltas (`prevInput`/`prevOutput`) | `agent_loop.go:146-151` | ✅ |
| `resolveWorkdirPath` guard on FSList/FSRead | `v2_handlers.go:1018-1031` | ✅ |
| `toolPartsOf` tool-history reconstruction | `agent_loop.go:95-103` | ✅ |
| All tests green | `go test ./...` | ✅ |

---

## Route gap analysis

SDK v2 has exactly **24 routes**. Our router has **21 of them**. Missing:

| Route | Purpose |
|---|---|
| `GET /api/session/{sessionID}/permission/request` | Session-scoped pending permission list |
| `POST /api/session/{sessionID}/question/request/{requestID}/reply` | Question answer |
| `POST /api/session/{sessionID}/question/request/{requestID}/reject` | Question reject |
| `GET /api/question/request` | Global pending question list |

---

## Critical

### Fix 1 — 4 missing v2 routes

**File:** `internal/server/v2_handlers.go`, `internal/server/router.go`

All four return 404 today, causing TUI hard failures on permission display
and question dialogs.

**Handler implementations:**

`GET /api/session/{sessionID}/permission/request` — session-scoped filter of
the global permission list:

```go
func (s *Server) handleV2SessionPermissionRequestList(w http.ResponseWriter, r *http.Request) {
    sessionID := r.PathValue("sessionID")
    if _, ok := s.store.GetSession(sessionID); !ok {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }
    list := s.perms.List()
    data := make([]any, 0)
    for _, req := range list {
        if req.SessionID != sessionID {
            continue
        }
        data = append(data, map[string]any{
            "id":        req.ID,
            "sessionID": req.SessionID,
            "tool":      req.Permission,
            "type":      req.Permission,
            "title":     "Allow tool: " + req.Permission,
            "metadata":  map[string]any{},
            "time":      map[string]any{"created": time.Now().UnixMilli()},
        })
    }
    writeJSON(w, http.StatusOK, map[string]any{"data": data})
}
```

`GET /api/question/request`, `POST /api/session/{sessionID}/question/request/{requestID}/reply`,
`POST /api/session/{sessionID}/question/request/{requestID}/reject` — opencode-go
does not implement the question system; return safe stubs:

```go
func (s *Server) handleV2QuestionRequestList(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}

func (s *Server) handleV2SessionQuestionReply(w http.ResponseWriter, r *http.Request) {
    sessionID := r.PathValue("sessionID")
    if _, ok := s.store.GetSession(sessionID); !ok {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{"data": true})
}

func (s *Server) handleV2SessionQuestionReject(w http.ResponseWriter, r *http.Request) {
    sessionID := r.PathValue("sessionID")
    if _, ok := s.store.GetSession(sessionID); !ok {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{"data": true})
}
```

**Routes to add to `router.go`:**

```go
mux.HandleFunc("GET /api/session/{sessionID}/permission/request",   s.handleV2SessionPermissionRequestList)
mux.HandleFunc("POST /api/session/{sessionID}/question/request/{requestID}/reply",  s.handleV2SessionQuestionReply)
mux.HandleFunc("POST /api/session/{sessionID}/question/request/{requestID}/reject", s.handleV2SessionQuestionReject)
mux.HandleFunc("GET /api/question/request", s.handleV2QuestionRequestList)
```

---

### Fix 2 — `sesQueue` memory leak on session delete

**File:** `internal/server/session_handlers.go:118`

`handleSessionDelete` calls `s.cancelSession(id)` (cancels context) but never
removes `sesQueue[id]`. After the aborted generation finishes, `processQueue`
terminates and leaves the `sessionWork` struct allocated for the lifetime of
the server process. For servers that create and delete many sessions (e.g. in
tests or automated pipelines), the map grows unboundedly.

Fix: after `cancelSession`, clean up the queue entry under the lock:

```go
s.cancelSession(id) // abort any in-flight generation
// Remove queue entry to prevent memory leak; cancel already signalled abort.
s.sesMu.Lock()
delete(s.sesQueue, id)
s.sesMu.Unlock()
```

This is safe: `cancelSession` signals the goroutine's context. The goroutine
reads `ctx.Done()` and returns from `runGenerationSync`, which then calls
`clearCancel(id)`. `processQueue` holds its own reference to `*sessionWork`
so deleting from the map does not race with the goroutine.

---

## Correctness

### Fix 3 — `handleV2SessionEvent` holds `sesMu` while reading store

**File:** `internal/server/v2_handlers.go:848`

The busy-session replay block at line 848 acquires `s.sesMu.Lock()` then calls
`s.store.Messages(sessionID)` at line 854. `store.Messages` acquires
`store.mu.RLock()`. While there is no deadlock risk (different mutexes), holding
the session-queue lock during a store read (which deep-copies all message parts)
blocks any concurrent `startOrQueue` or abort calls for all sessions, not just
this one. For a session with hundreds of parts, this can be tens of milliseconds.

Fix: read messages **before** acquiring `sesMu`, and capture `admitSeq` inside
the lock (fixing the hardcoded `1` at the same time — see Fix 4):

```go
// Read store outside the lock: no session-queue state needed here.
var replayMsgs []session.MessageWithParts
if msgs, ok := s.store.Messages(sessionID); ok {
    replayMsgs = msgs
}
for _, m := range replayMsgs {
    s.writeEvent(w, flusher, event.NewMessageUpdated(sessionID, m.Info, m.Info.Time.Completed != nil), event.KindEvent, "")
    for _, p := range m.Parts {
        s.writeEvent(w, flusher, event.NewMessagePartUpdated(sessionID, p, 0), event.KindEvent, "")
    }
}

s.sesMu.Lock()
work := s.sesQueue[sessionID]
isBusy := work != nil && work.running
admitSeq := int64(1)
if work != nil {
    admitSeq = work.admitSeq
}
s.sesMu.Unlock()

if isBusy {
    s.writeEvent(w, flusher, event.NewSessionStatus(sessionID, map[string]string{"type": "busy"}), event.KindEvent, "")
    for i := len(replayMsgs) - 1; i >= 0; i-- {
        if replayMsgs[i].Info.Role == "user" {
            text := partsText(replayMsgs[i].Parts, "text")
            s.writeEvent(w, flusher, event.NewSessionNextPrompted(sessionID, replayMsgs[i].Info.ID, text, "queue"), event.KindEvent, "")
            s.writeEvent(w, flusher, event.NewSessionNextPromptAdmitted(sessionID, replayMsgs[i].Info.ID, text, "queue", admitSeq), event.KindEvent, "")
            break
        }
    }
} else {
    s.writeEvent(w, flusher, event.NewSessionStatus(sessionID, map[string]string{"type": "idle"}), event.KindEvent, "")
    s.writeEvent(w, flusher, event.NewSessionIdle(sessionID), event.KindEvent, "")
}
```

Also remove the now-duplicated `if msgs, ok := s.store.Messages(sessionID); ok` block above — messages are already in `replayMsgs`.

---

### Fix 4 — Busy-session replay `admittedSeq` hardcoded to `1`

This is resolved by Fix 3 above (captured as `admitSeq := work.admitSeq` inside
the lock before unlock).

---

## Minor

### Fix 5 — `chatHistory` historical tool-calling turns missing `ReasoningContent`

**File:** `internal/server/agent_loop.go:62`

When a prior completed turn used a reasoning model, the reconstructed assistant
message that carries tool calls omits `ReasoningContent`. Some providers
(e.g. Claude with extended thinking) require the prior reasoning to appear in
the assistant message that contained the tool calls, or they return an error.

Fix: store the reasoning text in the `Part.Text` of reasoning parts (already
done). For history reconstruction, concatenate the reasoning parts and add to
the tool-call assistant message:

```go
// In the toolParts > 0 branch of chatHistory:
reasoningText := partsText(msg.Parts, "reasoning")
out = append(out, provider.ChatMessage{
    Role:             "assistant",
    ToolCalls:        tcs,
    ReasoningContent: reasoningText,
})
```

---

## Implementation order

1. Fix 1 — 4 missing routes (4 handlers + 4 router lines)
2. Fix 2 — sesQueue cleanup in `handleSessionDelete` (2 lines)
3. Fix 3+4 — `handleV2SessionEvent` refactor (lock scope + admitSeq)
4. Fix 5 — reasoning content in history reconstruction

## Files touched

| File | Fixes |
|---|---|
| `internal/server/v2_handlers.go` | 1 (handlers), 3+4 |
| `internal/server/router.go` | 1 (routes) |
| `internal/server/session_handlers.go` | 2 |
| `internal/server/agent_loop.go` | 5 |
