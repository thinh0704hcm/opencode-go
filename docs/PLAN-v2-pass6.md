# PLAN-v2-pass6.md

Sixth-pass fixes. All items confirmed by cross-reading post-pass-5 source
against `@opencode-ai/sdk` v2 TypeScript types at
`/tmp/opencode-sdk-ref/node_modules/@opencode-ai/sdk/dist/v2/gen/types.gen.d.ts`.

---

## Critical

### Fix 1 — `session.next.step.started` / `session.next.step.ended` never emitted

**File:** `internal/server/agent_loop.go`

The outer `for {}` loop in `runAgentLoop` runs one provider step per
iteration (text turn OR tool-call round trip) but never emits the step
lifecycle events. The TUI step counter, per-step cost display, and the
"Thinking…" spinner all depend on these events.

SDK shapes (confirmed at lines 3784–3835 in types.gen.d.ts):

```
EventSessionNextStepStarted.properties:
  { timestamp, sessionID, assistantMessageID, agent, model:{id, providerID, variant?}, snapshot? }

EventSessionNextStepEnded.properties:
  { timestamp, sessionID, assistantMessageID, finish, cost, tokens:{input,output,reasoning,cache:{read,write}} }
```

Our `SessionNextStepStartedProps` struct already matches. Our
`SessionNextStepEndedProps` also already matches. Both constructors exist in
`event.go` (`NewSessionNextStepStarted`, `NewSessionNextStepEnded`). The gap
is **call sites** — neither constructor is invoked in `agent_loop.go`.

Fix:

At the **top** of the `for {}` loop body (before `req` is built), emit
step-started using the `agent.Name`, `modelID`, and `s.provider.ID()`:

```go
s.bus.Publish(event.NewSessionNextStepStarted(sessionID, messageID, agent.Name, modelID, s.provider.ID()))
```

At the **bottom** of the `for {}` loop (two exit paths):

**Path A** — no tool calls (final text turn, line ~206 `if len(calls) == 0`):

```go
// Accumulate cost/tokens for this step.
// Usage already written to store by SetAssistantUsage during the stream.
if info, ok := s.store.MessageInfo(sessionID, messageID); ok {
    tok := info.Tokens
    var tokens event.SessionNextStepEndedTokens
    if tok != nil {
        tokens = event.SessionNextStepEndedTokens{
            Input:  tok.Input,
            Output: tok.Output,
        }
        tokens.Cache.Read  = tok.Cache.Read
        tokens.Cache.Write = tok.Cache.Write
    }
    s.bus.Publish(event.NewSessionNextStepEnded(sessionID, messageID, finishReason, info.Cost, tokens))
}
return finishReason
```

**Path B** — after all tool calls execute (end of `for` body, before loop
continues):

```go
if info, ok := s.store.MessageInfo(sessionID, messageID); ok {
    tok := info.Tokens
    var tokens event.SessionNextStepEndedTokens
    if tok != nil {
        tokens.Input  = tok.Input
        tokens.Output = tok.Output
        tokens.Cache.Read  = tok.Cache.Read
        tokens.Cache.Write = tok.Cache.Write
    }
    s.bus.Publish(event.NewSessionNextStepEnded(sessionID, messageID, "tool_calls", info.Cost, tokens))
}
commitLen = len(messages)
```

The `MessageInfo(sessionID, messageID)` helper already exists at
`store.go:610`.

---

### Fix 2 — `session.next.reasoning.*` events never emitted

**Files:** `internal/event/event.go`, `internal/server/agent_loop.go`

`agent_loop.go:170-172` streams reasoning deltas via `emitDelta(..., "reasoning",
...)` which only updates the store and fires `message.part.delta` /
`message.part.updated`. The three `session.next.reasoning.*` SSE events are
not emitted at all. The TUI renders the reasoning panel from these events.

**Step 1 — Add to `event.go`:**

Constants:

```go
TypeSessionNextReasoningStarted = "session.next.reasoning.started"
TypeSessionNextReasoningDelta   = "session.next.reasoning.delta"
TypeSessionNextReasoningEnded   = "session.next.reasoning.ended"
```

Props structs:

```go
type SessionNextReasoningStartedProps struct {
    Timestamp          int64  `json:"timestamp"`
    SessionID          string `json:"sessionID"`
    AssistantMessageID string `json:"assistantMessageID"`
    ReasoningID        string `json:"reasoningID"`
}

type SessionNextReasoningDeltaProps struct {
    Timestamp          int64  `json:"timestamp"`
    SessionID          string `json:"sessionID"`
    AssistantMessageID string `json:"assistantMessageID"`
    ReasoningID        string `json:"reasoningID"`
    Delta              string `json:"delta"`
}

type SessionNextReasoningEndedProps struct {
    Timestamp          int64  `json:"timestamp"`
    SessionID          string `json:"sessionID"`
    AssistantMessageID string `json:"assistantMessageID"`
    ReasoningID        string `json:"reasoningID"`
    Text               string `json:"text"`
}
```

Constructors: `NewSessionNextReasoningStarted`, `NewSessionNextReasoningDelta`,
`NewSessionNextReasoningEnded` — parallel to the `Text` equivalents.

Add all three to `eventSessionID`'s type-switch in `v2_handlers.go`.

**Step 2 — Emit in `agent_loop.go`:**

Mirror the `textID` / `textBuf` pattern with `reasoningID` / `reasoningBuf`:

```go
var reasoningID  string
var reasoningBuf strings.Builder

// inside the stream loop:
if chunk.ReasoningDelta != "" {
    if reasoningID == "" {
        reasoningID = event.NewID("rsn")
        s.bus.Publish(event.NewSessionNextReasoningStarted(sessionID, messageID, reasoningID))
    }
    reasoningBuf.WriteString(chunk.ReasoningDelta)
    s.bus.Publish(event.NewSessionNextReasoningDelta(sessionID, messageID, reasoningID, chunk.ReasoningDelta))
    s.emitDelta(sessionID, messageID, "reasoning", chunk.ReasoningDelta)
}

// after the stream loop (before return / before tools):
if reasoningID != "" {
    s.bus.Publish(event.NewSessionNextReasoningEnded(sessionID, messageID, reasoningID, reasoningBuf.String()))
    reasoningID = ""
    reasoningBuf.Reset()
}
```

Reset `reasoningID`/`reasoningBuf` in the retry reset block alongside
`textID`/`textBuf`.

---

### Fix 3 — `finishGeneration` never calls `FinishOpenParts`

**File:** `internal/server/generation.go:169`

`FinishOpenParts(sessionID, messageID string) []Part` exists in `store.go:591`
and sets `Time.End` on any text/reasoning part whose end is still nil.
`finishGeneration` calls `CompleteAssistantMessage` but skips it, so a crashed
or aborted stream leaves parts with no `Time.End`, and the TUI shows a
perpetual "Thinking…" spinner for the turn.

Fix:

```go
func (s *Server) finishGeneration(sessionID, messageID string) {
    updated := s.store.FinishOpenParts(sessionID, messageID)
    for i := range updated {
        s.bus.Publish(event.NewMessagePartUpdated(sessionID, updated[i], time.Now().UnixMilli()))
    }
    info, ok := s.store.CompleteAssistantMessage(sessionID, messageID)
    if !ok {
        return
    }
    s.bus.Publish(event.NewMessageUpdated(sessionID, info, true))
    s.store.PersistSession(sessionID)
}
```

---

## Correctness

### Fix 4 — `mapToV2Message` reasoning parts missing `time` and `metadata`

**File:** `internal/server/v2_handlers.go:524`

The SDK `ReasoningPart` type (line 272) requires `time: {start, end?}` and
has optional `metadata`. We emit only `{type, id, text}`.

Fix:

```go
case "reasoning":
    if p.Text == "" {
        continue
    }
    rp := map[string]any{
        "type": "reasoning",
        "id":   p.ID,
        "text": p.Text,
    }
    if p.Time != nil {
        var endMS any
        if p.Time.End != nil {
            endMS = *p.Time.End
        }
        rp["time"] = map[string]any{"start": p.Time.Start, "end": endMS}
    }
    if p.Metadata != nil {
        rp["metadata"] = p.Metadata
    }
    content = append(content, rp)
```

Verify `session.Part` has `.Time` (shared with text parts) and `.Metadata`
fields — both are already used by the `tool` case.

---

### Fix 5 — `handleV2PermissionRequestList` always returns empty array

**File:** `internal/server/v2_handlers.go:907`

The TUI "Permissions" panel calls `GET /api/permission/request` on load to
show existing pending permission gates. The stub always returns `{data: []}`.

Fix: map `s.perms.List()` to a v2-shaped array:

```go
func (s *Server) handleV2PermissionRequestList(w http.ResponseWriter, r *http.Request) {
    list := s.perms.List()
    data := make([]any, 0, len(list))
    for _, req := range list {
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

---

### Fix 6 — `handleV2SessionEvent` replay missing prompted/admitted for busy session

**File:** `internal/server/v2_handlers.go:833`

When `work != nil && work.running`, the replay block only emits
`session.status{busy}`. On reconnect, the TUI loses the current prompt text
(the prompt bubble goes blank).

Fix: after the busy status, find the last user message and re-emit the prompt
events so the TUI can render the running turn's prompt:

```go
if work != nil && work.running {
    s.writeEvent(w, flusher, event.NewSessionStatus(sessionID, map[string]string{"type": "busy"}), event.KindEvent, "")
    // Re-emit the prompt for the current turn so reconnecting clients
    // see the prompt bubble.
    if msgs, ok := s.store.Messages(sessionID); ok {
        for i := len(msgs) - 1; i >= 0; i-- {
            if msgs[i].Info.Role == "user" {
                text := partsText(msgs[i].Parts, "text")
                s.writeEvent(w, flusher, event.NewSessionNextPrompted(sessionID, msgs[i].Info.ID, text, "queue"), event.KindEvent, "")
                s.writeEvent(w, flusher, event.NewSessionNextPromptAdmitted(sessionID, msgs[i].Info.ID, text, "queue", 1), event.KindEvent, "")
                break
            }
        }
    }
}
```

---

## Minor

### Fix 7 — `handleV2FSList` and `handleV2FSRead` stubs

**File:** `internal/server/v2_handlers.go:969`

Both return stubs. The TUI file-browser uses these.

`FSList` — list entries in `path` query param (default `s.workdir`):

```go
func (s *Server) handleV2FSList(w http.ResponseWriter, r *http.Request) {
    path := r.URL.Query().Get("path")
    if path == "" {
        path = s.workdir
    }
    entries, err := os.ReadDir(path)
    if err != nil {
        writeError(w, http.StatusBadRequest, err.Error())
        return
    }
    data := make([]map[string]any, 0, len(entries))
    for _, e := range entries {
        data = append(data, map[string]any{
            "name":  e.Name(),
            "type":  map[bool]string{true: "directory", false: "file"}[e.IsDir()],
        })
    }
    writeJSON(w, http.StatusOK, map[string]any{"data": data})
}
```

`FSRead` — read file content from `path` query param:

```go
func (s *Server) handleV2FSRead(w http.ResponseWriter, r *http.Request) {
    path := r.URL.Query().Get("path")
    if path == "" {
        writeError(w, http.StatusBadRequest, "path required")
        return
    }
    content, err := os.ReadFile(path)
    if err != nil {
        writeError(w, http.StatusNotFound, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{"data": string(content)})
}
```

Add `"os"` to `v2_handlers.go` imports if not already present.

---

## Implementation order

1. Fix 3 — `FinishOpenParts` in `finishGeneration` (smallest, highest UX impact)
2. Fix 1 — emit `step.started`/`step.ended` in `agent_loop.go`
3. Fix 2 — add reasoning event types/constructors, emit in `agent_loop.go`
4. Fix 4 — reasoning `time`/`metadata` in `mapToV2Message`
5. Fix 5 — permission request list real data
6. Fix 6 — busy-session replay with prompted/admitted
7. Fix 7 — FSList/FSRead real implementations

## Files touched

| File | Fixes |
|---|---|
| `internal/server/generation.go` | 3 |
| `internal/server/agent_loop.go` | 1, 2 |
| `internal/event/event.go` | 2 |
| `internal/server/v2_handlers.go` | 2 (eventSessionID), 4, 5, 6, 7 |
