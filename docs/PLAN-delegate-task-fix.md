# Bug Analysis: delegate / task broken in TUI

**Source of truth:** `@opencode-ai/sdk@1.17.4` — `dist/v2/gen/types.gen.d.ts`

---

## Root cause summary

The Go server emits the **wrong part type** for delegate/task tool calls and has **wrong property
shapes** on several events the TUI consumes. There are five distinct bugs, listed from most
critical to least.

---

## Bug 1 — `subtask` part not emitted (PRIMARY BREAK)

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 264
export type SubtaskPart = {
    id: string;
    sessionID: string;
    messageID: string;
    type: "subtask";
    prompt: string;
    description: string;
    agent: string;
    model?: { providerID: string; modelID: string; };
    command?: string;
};
```

`SubtaskPart` is a first-class member of the `Part` union (alongside `TextPart`, `ToolPart`,
etc.). The TUI renders `type === "subtask"` as a sub-agent panel that links to the child
session. It renders `type === "tool"` as a generic tool call with input/output/spinner.

### What Go currently does

`runAgentLoop` calls `s.store.AppendToolPart(..., "running", ...)` and
`s.store.AppendToolPart(..., "completed", ...)` for ALL tool names including `"delegate"` and
`"task"`. The emitted `message.part.updated` event carries a `ToolPart` (type `"tool"`).

The TUI receives this and renders delegate/task as if it were a bash or read call — no
sub-agent panel, no link to the child session, no progress while the child runs.

### Fix

In `runAgentLoop`, before executing a `delegate` or `task` call:

1. **Do NOT emit a `tool` part** for delegate/task.
2. **Emit a `subtask` part** on the parent message immediately when the call starts.
3. After `runDelegated` returns, **update the subtask part** to include the output (or just
   leave it static — the TUI navigates to the child session for full detail).

The subtask part payload (fields required by the SDK type):

```go
type SubtaskPart struct {
    ID        string `json:"id"`
    SessionID string `json:"sessionID"`
    MessageID string `json:"messageID"`
    Type      string `json:"type"`      // always "subtask"
    Prompt    string `json:"prompt"`
    Desc      string `json:"description"`
    Agent     string `json:"agent"`
    Model     *struct {
        ProviderID string `json:"providerID"`
        ModelID    string `json:"modelID"`
    } `json:"model,omitempty"`
}
```

Store-side: add `AppendSubtaskPart(sessionID, messageID, prompt, description, agent string, providerID, modelID string) (Part, bool)` to `session.Store`.

Event-side: publish `message.part.updated` with the subtask part immediately when the delegate
starts; no second publish is needed (the TUI navigates to the child session for progress).

**Do NOT also publish `session.next.tool.input.started` / `.ended` / `.called` / `.success`
/ `.failed` for delegate/task calls.** Those events are for tool calls, not sub-agent
delegation. Emitting them for delegate/task confuses the TUI's rendering pipeline.

---

## Bug 2 — `permission.asked` properties shape is wrong

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 1155
type: "permission.asked";
properties: {
    id: string;
    sessionID: string;
    permission: string;      // the tool/permission name
    patterns: Array<string>; // patterns being requested
    metadata: { [key: string]: unknown; };
    always: Array<string>;   // always-allow list for this tool
    tool?: {                 // which tool call triggered this
        messageID: string;
        callID: string;
    };
};
```

### What Go currently emits

```go
// internal/server/agent_loop.go — runAgentLoop
requestObj := map[string]any{
    "id":         preq.ID,
    "type":       call.Name,    // ❌ should be "permission", value is the name
    "tool":       call.Name,    // ❌ this field has wrong type (string vs {messageID,callID})
    "permission": call.Name,    // ✅ correct field name, correct value
    "pattern":    pattern,      // ❌ should be "patterns" (array), not "pattern" (string)
    "always":     []any{},      // ✅ correct
    "patterns":   []any{pattern}, // ✅ correct (but also has "pattern" dupe above)
    "sessionID":  permSessID,   // ✅ correct
    "messageID":  messageID,    // ❌ should be inside tool:{} not top-level
    "callID":     call.ID,      // ❌ should be inside tool:{} not top-level
    "title":      "...",        // ❌ not in the SDK type
    "metadata":   map[string]any{},  // ✅ correct
    "call":       map[string]any{...},// ❌ not in the SDK type
    "time":       ...,          // ❌ not in the SDK type
}
askObj := map[string]any{"id": preq.ID, "request": requestObj}
for k, v := range requestObj { askObj[k] = v }  // ❌ messy merge
s.bus.Publish(event.NewPermissionAsked(askObj))  // ❌ publishes the whole blob as Properties
```

The final `Properties` object is a deeply wrong shape. The `type` field shadows the event
envelope's own `type`. The `tool` field has type `string` but SDK expects `{messageID, callID}`.
The `pattern` (singular) field conflicts with `patterns` (plural). Extra fields (`title`, `call`,
`request`, `time`) are not in the SDK contract.

### Fix

Replace the entire `permission.asked` construction with the SDK-exact shape:

```go
s.bus.Publish(event.NewPermissionAsked(map[string]any{
    "id":         preq.ID,
    "sessionID":  permSessID,
    "permission": call.Name,
    "patterns":   []any{pattern},
    "metadata":   map[string]any{},
    "always":     []any{},
    "tool": map[string]any{
        "messageID": messageID,
        "callID":    call.ID,
    },
}))
```

Also fix the `permission.updated` event — its shape is used by the legacy v1 TUI but should
not mix in the extra fields either. Check what `permission.updated` fields the TUI actually
reads and trim accordingly.

---

## Bug 3 — `session.next.tool.success` missing `structured` field

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 928
type: "session.next.tool.success";
properties: {
    timestamp: number;
    sessionID: string;
    assistantMessageID: string;
    callID: string;
    structured: { [key: string]: unknown; };  // REQUIRED
    content: Array<ToolTextContent | ToolFileContent>;
    outputPaths?: Array<string>;
    result?: unknown;
    provider: { executed: boolean; metadata?: {...}; };
};
```

### What Go currently emits

```go
// internal/event/event.go
type SessionNextToolSuccessProps struct {
    Timestamp          int64  `json:"timestamp"`
    SessionID          string `json:"sessionID"`
    AssistantMessageID string `json:"assistantMessageID"`
    CallID             string `json:"callID"`
    Content []struct {
        Type string `json:"type"`
        Text string `json:"text"`
    } `json:"content"`
    Provider struct {
        Executed bool `json:"executed"`
    } `json:"provider"`
    // ❌ missing: structured
}
```

`structured` is missing. If the TUI does `properties.structured.someKey`, it gets a crash or
undefined access.

### Fix

Add `Structured map[string]any \`json:"structured"\`` to `SessionNextToolSuccessProps`, always
set it to `map[string]any{}` (empty object).

---

## Bug 4 — `session.next.tool.failed` wrong error shape

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 2485
export type SessionErrorUnknown = {
    type: "unknown";
    message: string;
};

// Used in session.next.tool.failed:
error: SessionErrorUnknown;  // { type: "unknown", message: string }
```

### What Go currently emits

```go
Error: struct {
    Type    string `json:"type"`
    Message string `json:"message"`
}{Type: "tool_execution_error", Message: errMsg},  // ❌ type must be "unknown"
```

The discriminant `type` must be `"unknown"` per the SDK union. Sending `"tool_execution_error"`
means the TUI's pattern matching on the error type produces a no-match / fallthrough.

### Fix

```go
Error: struct {
    Type    string `json:"type"`
    Message string `json:"message"`
}{Type: "unknown", Message: errMsg},
```

---

## Bug 5 — `session.next.step.failed` wrong error shape

Same issue as Bug 4. `session.next.step.failed` also uses `error: SessionErrorUnknown`:

```typescript
type: "session.next.step.failed";
properties: {
    ...
    error: SessionErrorUnknown;  // { type: "unknown", message }
};
```

Current Go `SessionNextStepFailedProps`:
```go
Error: struct {
    Type    string `json:"type"`
    Message string `json:"message"`
}{Type: errType, Message: errMsg}
```

This is called from `s.bus.Publish(event.NewSessionNextStepFailed(...))` — the `errType`
argument should always be `"unknown"` or the function signature should be simplified to just
`message string`.

### Fix

Remove the `errType` parameter from `NewSessionNextStepFailed` (or always pass `"unknown"`).

---

## Bug 6 — Session `slug` field missing

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 64
export type Session = {
    id: string;
    slug: string;   // REQUIRED
    projectID: string;
    ...
};
```

### What Go currently emits

`session.Store.GetSession()` returns a `session.Session` struct that has no `slug` field. When
the TUI reads `session.slug` (used for URL routing and display in newer clients), it gets
`undefined`.

### Fix

Add `Slug string \`json:"slug"\`` to `session.Session`. Derive it deterministically from the
session ID: `slug = id[:8]` (first 8 chars of the base62 ID). Set it in `CreateSession`.

---

## Bug 7 — AssistantMessage missing `agent` field

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 213
export type AssistantMessage = {
    id: string;
    ...
    agent: string;  // REQUIRED in v2 (was absent in v1)
    ...
};
```

### What Go currently emits

`session.MessageInfo` (the assistant message info object) likely does not include `agent` at
the top level — it may be inside a nested object or not present at all.

### Fix

Add `Agent string \`json:"agent"\`` to `session.MessageInfo`. Populate it from the `agentName`
passed to `NewAssistantMessage`.

---

## Event sequence: what it should look like for delegate/task

Using the SDK as source of truth, the correct sequence for a delegate/task call is:

**On the PARENT session's assistant message:**

```
session.next.step.started       { sessionID: parent, assistantMessageID: parentAsst }
  session.next.text.started     { sessionID: parent, ... }    [if AI emitted text before calling]
  session.next.text.delta       { ... }
  session.next.text.ended       { ... }
  session.next.tool.input.started { sessionID: parent, callID, name: "task" }
  session.next.tool.input.ended   { sessionID: parent, callID, text: "{...}" }
  session.next.tool.called        { sessionID: parent, callID, tool: "task", input: {...} }
  message.part.updated            { part: SubtaskPart{ type:"subtask", agent, prompt, ... } }
  [sub-session runs — emits its own events on child sessionID — parent stream is quiet]
  message.part.updated            { part: SubtaskPart{ ... same id, updated? } }
  [NO session.next.tool.success / .failed for delegate/task]
session.next.step.ended         { finish: "tool_calls" or "stop", ... }
```

**On the CHILD (sub) session:**
```
session.created                 { info: Session{ parentID: parentSessID, ... } }
session.status                  { sessionID: child, status: { type: "busy" } }
message.updated                 { sessionID: child, info: UserMessage }
message.updated                 { sessionID: child, info: AssistantMessage (empty) }
session.next.step.started       { sessionID: child, ... }
  session.next.text.started / delta / ended
  [any tool calls the child makes]
session.next.step.ended         { sessionID: child, ... }
message.part.updated            { sessionID: child, part: StepFinishPart }
message.updated                 { sessionID: child, info: AssistantMessage (completed) }
session.status                  { sessionID: child, status: { type: "idle" } }
session.idle                    { sessionID: child }
```

The TUI connects child to parent via `Session.parentID` and `SubtaskPart.agent`. No
`tool.success` / `tool.failed` events fire on the parent for delegation.

---

## Fix priority

| # | Bug | Impact |
|---|-----|--------|
| 1 | `subtask` part not emitted | TUI renders wrong UI, sub-agent panel never appears |
| 2 | `permission.asked` shape wrong | Permission dialog never renders correctly |
| 3 | `structured` missing in tool.success | TUI crashes or shows broken tool results |
| 4 | `tool.failed` error type wrong | Error rendering broken |
| 5 | `step.failed` error type wrong | Error rendering broken |
| 6 | Session `slug` missing | URL routing broken in newer TUI |
| 7 | AssistantMessage `agent` missing | Agent label missing in TUI header |

---

## Files to change

| File | Change |
|------|--------|
| `internal/session/store.go` | Add `AppendSubtaskPart`; add `Slug` to `Session`; add `Agent` to `MessageInfo` |
| `internal/session/session.go` | Add `Slug` field; populate in `CreateSession` |
| `internal/event/event.go` | Add `Structured` to `SessionNextToolSuccessProps`; fix error type in tool.failed and step.failed |
| `internal/server/agent_loop.go` | Skip tool/success/failed events for delegate/task; emit subtask part instead |
| `internal/server/delegate_tools.go` | No longer calls `AppendToolPart`; uses `AppendSubtaskPart` |
