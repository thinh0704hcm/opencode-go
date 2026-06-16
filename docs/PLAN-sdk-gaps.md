# SDK Gap Analysis — Deviations not in PLAN-sdk-drop-in.md or PLAN-delegate-task-fix.md

**Source of truth:** `@opencode-ai/sdk@1.17.4` — `dist/gen/types.gen.d.ts` (v1) and
`dist/v2/gen/types.gen.d.ts` (v2).

This document covers deviations found by comparing the full SDK type surface against the
current Go implementation. Bugs already documented in the two existing plans are excluded.

---

## Severity classification

- **S1 — Breaking**: TUI renders wrong or crashes.
- **S2 — Degraded**: Feature partially works but data is missing or malformed.
- **S3 — Minor**: Extra or missing optional fields; additive deviation.

---

## Gap 1 — v1 `Session` missing `version` field  [S1]

### What the SDK defines

```typescript
// dist/gen/types.gen.d.ts line 465
export type Session = {
    id: string;
    projectID: string;
    directory: string;
    parentID?: string;
    title: string;
    version: string;   // REQUIRED — TUI uses this for cache invalidation
    time: { created: number; updated: number; compacting?: number; };
    summary?: { additions: number; deletions: number; files: number; diffs?: ... };
    share?: { url: string; };
    revert?: { messageID: string; partID?: string; snapshot?: string; diff?: string; };
};
```

### What Go currently emits

`session.Session` struct in `internal/session/session.go`:
```go
type Session struct {
    ID        string      `json:"id"`
    Slug      string      `json:"slug"`
    ParentID  string      `json:"parentID,omitempty"`
    Title     string      `json:"title"`
    Directory string      `json:"directory"`
    Time      SessionTime `json:"time"`
    // ❌ missing: version, projectID, summary, share, revert
}
```

`version` is required by the v1 TUI. When Go emits `session.created` / `session.updated`, the
`info` object has no `version` field. The TUI uses it to detect when it needs to reload session
state.

### Fix

Add `Version string \`json:"version"\`` to `session.Session`. Set it to the server version
constant (e.g., `"1.17.4"` or whatever `internal/server/server.go` exports as `Version`).
Also add `ProjectID string \`json:"projectID"\`` (derive from `filepath.Base(Directory)` like
`mapToV2Info` already does).

**Files:** `internal/session/session.go`, `internal/session/store.go` (`CreateSession`,
`CreateSessionWithID`)

---

## Gap 2 — v2 message API: wrong discriminant field (`role` vs `type`)  [S1]

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 3292
export type SessionMessageUser = {
    id: string;
    time: { created: number; };
    text: string;
    files?: Array<PromptFileAttachment>;
    agents?: Array<PromptAgentAttachment>;
    type: "user";   // discriminant is "type", NOT "role"
    metadata?: { [key: string]: unknown; };
};

export type SessionMessageAssistant = {
    id: string;
    time: { created: number; completed?: number; };
    type: "assistant";  // discriminant is "type", NOT "role"
    agent: string;
    model: { id: string; providerID: string; variant?: string; };
    content: Array<SessionMessageAssistantText | SessionMessageAssistantReasoning | SessionMessageAssistantTool>;
    snapshot?: { start?: string; end?: string; };
    finish?: string;
    cost?: number;
    tokens?: { input; output; reasoning; cache: { read; write }; };
    error?: SessionErrorUnknown;
    metadata?: { [key: string]: unknown; };
};

export type SessionMessage = SessionMessageAgentSwitched | SessionMessageModelSwitched
    | SessionMessageUser | SessionMessageSynthetic | SessionMessageSystem
    | SessionMessageShell | SessionMessageAssistant | SessionMessageCompaction;
```

### What Go currently emits

`mapToV2Message` in `internal/server/v2_handlers.go` emits `role: "user"` and `role: "assistant"` (v1 field), not `type`. The v2 TUI switches on `type`.

Also the user message shape has extra fields (`agent`, `model`) that don't exist in `SessionMessageUser`. The user message in v2 has only `text`, `files?`, `agents?` at message level — no `agent` or `model`.

### Fix

Rewrite `mapToV2Message`:
- User: return `{ id, type: "user", time: {created}, text, metadata: {} }`
- Assistant: return `{ id, type: "assistant", time: {created, completed?}, agent, model: {id: modelID, providerID}, content: [...], finish?, cost?, tokens?, error?, snapshot?, metadata: {} }`

**File:** `internal/server/v2_handlers.go` → `mapToV2Message`

---

## Gap 3 — v2 message API: tool state wrong shape  [S1]

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 3361–3395
export type SessionMessageToolStateRunning = {
    status: "running";
    input: { [key: string]: unknown; };
    structured: { [key: string]: unknown; };  // REQUIRED
    content: Array<ToolTextContent | ToolFileContent>;  // REQUIRED
};
export type SessionMessageToolStateCompleted = {
    status: "completed";
    input: { [key: string]: unknown; };
    content: Array<ToolTextContent | ToolFileContent>;  // REQUIRED (not "output" string)
    outputPaths?: Array<string>;
    structured: { [key: string]: unknown; };  // REQUIRED
    attachments?: Array<PromptFileAttachment>;
    result?: unknown;
};
export type SessionMessageToolStateError = {
    status: "error";
    input: { [key: string]: unknown; };
    content: Array<ToolTextContent | ToolFileContent>;  // REQUIRED
    structured: { [key: string]: unknown; };  // REQUIRED
    error: SessionErrorUnknown;  // REQUIRED — { type: "unknown", message: string }
    result?: unknown;
};
```

### What Go currently emits

```go
// internal/server/v2_handlers.go mapToV2Message, case "tool"
state := map[string]any{
    "status":   p.State.Status,
    "input":    p.State.Input,
    "title":    p.State.Title,
    "metadata": p.State.Metadata,
}
if p.State.Status == "error" {
    state["error"] = p.State.Output  // ❌ string, should be {type:"unknown",message:...}
} else {
    state["output"] = p.State.Output  // ❌ "output" string, should be "content": [{type:"text",text:...}]
}
// ❌ missing: structured, content
// ❌ "title" and "metadata" are not in the v2 tool state types
```

Also, the tool item in Go emits extra `callID`, `tool`, `sessionID`, `messageID` fields (these
don't exist on `SessionMessageAssistantTool` — only `id`, `name`, `provider?`, `state`, `time`).

The v2 tool item shape is:
```typescript
export type SessionMessageAssistantTool = {
    type: "tool";
    id: string;
    name: string;
    provider?: { executed: boolean; metadata?: {...}; resultMetadata?: {...}; };
    state: SessionMessageToolState*;
    time: { created: number; ran?: number; completed?: number; pruned?: number; };
};
```

### Fix

Rewrite the `case "tool"` branch in `mapToV2Message`:
```go
var state map[string]any
switch p.State.Status {
case "running":
    state = map[string]any{
        "status":     "running",
        "input":      p.State.Input,
        "structured": map[string]any{},
        "content":    []any{},
    }
case "completed":
    content := []any{}
    if p.State.Output != "" {
        content = []any{map[string]any{"type": "text", "text": p.State.Output}}
    }
    state = map[string]any{
        "status":     "completed",
        "input":      p.State.Input,
        "content":    content,
        "structured": map[string]any{},
    }
case "error":
    content := []any{}
    if p.State.Output != "" {
        content = []any{map[string]any{"type": "text", "text": p.State.Output}}
    }
    state = map[string]any{
        "status":     "error",
        "input":      p.State.Input,
        "content":    content,
        "structured": map[string]any{},
        "error": map[string]any{
            "type":    "unknown",
            "message": p.State.Output,
        },
    }
}
var toolTime map[string]any
if p.State.Time != nil {
    toolTime = map[string]any{"created": p.State.Time.Start}
    if p.State.Time.End != nil {
        toolTime["completed"] = *p.State.Time.End
    }
}
content = append(content, map[string]any{
    "type":  "tool",
    "id":    p.ID,
    "name":  p.Tool,
    "state": state,
    "time":  toolTime,
})
```

**File:** `internal/server/v2_handlers.go` → `mapToV2Message`

---

## Gap 4 — v2 session list: `model.id` format wrong  [S2]

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 3228
export type SessionV2Info = {
    ...
    model?: {
        id: string;         // just the model ID (e.g., "claude-3-5-sonnet-20241022")
        providerID: string; // e.g., "anthropic"
        variant?: string;
    };
    ...
};
```

### What Go currently emits

```go
// internal/server/v2_handlers.go mapToV2Info line 188
ID: p.ID + "/" + m.Info.ModelID,  // ❌ "anthropic/claude-3-5-sonnet..." — wrong
ProviderID: m.Info.ProviderID,    // ✅ correct
```

`model.id` should be just `m.Info.ModelID`, not `providerID + "/" + modelID`.

### Fix

```go
info.Model = &struct {
    ID         string `json:"id"`
    ProviderID string `json:"providerID"`
}{
    ID:         m.Info.ModelID,       // was: p.ID + "/" + m.Info.ModelID
    ProviderID: m.Info.ProviderID,
}
```

**File:** `internal/server/v2_handlers.go` → `mapToV2Info` (line ~188)

---

## Gap 5 — v2 model list: `cost` is object instead of array; missing `api` field  [S2]

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 2372
export type ModelV2Info = {
    id: string;
    providerID: string;
    name: string;
    api: {                 // REQUIRED
        id: string;
        type: "aisdk";
        package: string;
        url?: string;
    } | { id: string; type: "native"; ... };
    capabilities: { tools: boolean; input: Array<string>; output: Array<string>; };
    request: { headers: {...}; body: {...}; };
    variants: Array<{...}>;
    time: { released: number };
    cost: Array<{          // REQUIRED — array, not flat object
        tier?: { type: "context"; size: number; };
        input: number;
        output: number;
        cache: { read: number; write: number; };
    }>;
    status: "alpha" | "beta" | "deprecated" | "active";
    enabled: boolean;
    limit: { context: number; input?: number; output: number; };
};
```

### What Go currently emits

```go
type modelV2Info struct {
    ID           string `json:"id"`
    ProviderID   string `json:"providerID"`
    Name         string `json:"name"`
    Enabled      bool   `json:"enabled"`
    Capabilities struct{ Tools bool; Input []string; Output []string } `json:"capabilities"`
    Limit        struct{ Context int; Output int } `json:"limit"`
    Cost         struct{   // ❌ flat object, SDK wants Array
        Input  float64 `json:"input"`
        Output float64 `json:"output"`
        Cache  struct{ Read, Write float64 } `json:"cache"`
    } `json:"cost"`
    // ❌ missing: api, request, variants, time, status
}
```

### Fix

Change `Cost` to `Cost []any \`json:"cost"\`` and populate with a single-element array.
Add stub `API`, `Request`, `Variants`, `Time`, `Status` fields. Since Go doesn't have rich
model metadata, use sensible defaults (`status: "active"`, empty `request`, stub `api`).

**File:** `internal/server/v2_handlers.go` → `handleV2ModelList`, `modelV2Info`

---

## Gap 6 — v2 provider list: missing `api` and `request` fields  [S2]

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 3469
export type ProviderV2Info = {
    id: string; name: string;
    enabled: false | { via: "env"; name: string; } | { via: "credential"; ... } | { via: "custom"; ... };
    env: Array<string>;
    api: { type: "aisdk"; package: string; url?: string; } | { type: "native"; ... };  // REQUIRED
    request: { headers: {...}; body: {...}; };  // REQUIRED
};
```

### What Go currently emits

```go
type providerV2Info struct {
    ID      string   `json:"id"`
    Name    string   `json:"name"`
    Enabled any      `json:"enabled"`
    Env     []string `json:"env"`
    // ❌ missing: api, request
}
```

### Fix

Add stub `API` and `Request` fields:
```go
type providerV2Info struct {
    ID      string   `json:"id"`
    Name    string   `json:"name"`
    Enabled any      `json:"enabled"`
    Env     []string `json:"env"`
    API     struct {
        Type    string `json:"type"`
        Package string `json:"package"`
    } `json:"api"`
    Request struct {
        Headers map[string]string `json:"headers"`
        Body    map[string]any    `json:"body"`
    } `json:"request"`
}
```
Set `API.Type = "aisdk"` and `API.Package = ""` (empty, since Go doesn't use npm).

**File:** `internal/server/v2_handlers.go` → `handleV2ProviderList`, `handleV2ProviderGet`

---

## Gap 7 — v2 agent list: missing `request` field  [S2]

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 3205
export type AgentV2Info = {
    id: string;
    request: { headers: {...}; body: {...}; };  // REQUIRED
    mode: "subagent" | "primary" | "all";
    hidden: boolean;
    permissions: PermissionV2Ruleset;
    system?: string; description?: string; color?: string; steps?: number;
    model?: { id: string; providerID: string; variant?: string; };
};
```

### What Go currently emits

```go
type agentV2Info struct {
    ID          string `json:"id"`
    Mode        string `json:"mode"`
    Hidden      bool   `json:"hidden"`
    Description string `json:"description,omitempty"`
    System      string `json:"system,omitempty"`
    Permissions []any  `json:"permissions"`
    // ❌ missing: request
}
```

### Fix

Add:
```go
Request struct {
    Headers map[string]string `json:"headers"`
    Body    map[string]any    `json:"body"`
} `json:"request"`
```
Initialize as empty maps.

**File:** `internal/server/v2_handlers.go` → `handleV2AgentList`, `agentV2Info`

---

## Gap 8 — v2 permission request list: wrong field names  [S1]

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 3558
export type PermissionV2Request = {
    id: string;
    sessionID: string;
    action: string;                // the permission/tool name
    resources: Array<string>;      // patterns being requested (not "tool" or "type")
    save?: Array<string>;
    metadata?: { [key: string]: unknown; };
    source?: PermissionV2Source;   // { type: "tool"; messageID: string; callID: string; }
};
```

### What Go currently emits

```go
// internal/server/v2_handlers.go handleV2PermissionRequestList
data = append(data, map[string]any{
    "id":        req.ID,
    "sessionID": req.SessionID,
    "tool":      req.Permission,  // ❌ should be "action"
    "type":      req.Permission,  // ❌ not in SDK type
    "title":     "Allow tool: " + req.Permission,  // ❌ not in SDK type
    "metadata":  map[string]any{},
    "time":      map[string]any{"created": ...},   // ❌ not in SDK type
    // ❌ missing: "resources" (array), "source"
})
```

### Fix

```go
data = append(data, map[string]any{
    "id":        req.ID,
    "sessionID": req.SessionID,
    "action":    req.Permission,
    "resources": []string{},  // or populate from req.Patterns
    "metadata":  map[string]any{},
    // omit save, source unless available
})
```

Also fix `handleV2SessionPermissionRequestList` which has the same wrong shape.

**Files:** `internal/server/v2_handlers.go` → `handleV2PermissionRequestList`,
`handleV2SessionPermissionRequestList`

---

## Gap 9 — `permission.v2.asked` event not emitted  [S1]

### What the SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 4276
export type EventPermissionV2Asked = {
    id: string;
    type: "permission.v2.asked";
    properties: {
        id: string;
        sessionID: string;
        action: string;
        resources: Array<string>;
        save?: Array<string>;
        metadata?: { [key: string]: unknown; };
        source?: PermissionV2Source;
    };
};
```

### What Go currently does

Go only emits `permission.asked` (v1 shape). The v2 TUI listens for `permission.v2.asked` to
show permission dialogs. Without it, tool-permission prompts are silently dropped in v2 clients.

### Fix

In `internal/server/agent_loop.go`, after publishing `permission.asked`, also publish
`permission.v2.asked`:

```go
s.bus.Publish(Event{
    ID:   newID("evt"),
    Type: "permission.v2.asked",
    Properties: map[string]any{
        "id":        preq.ID,
        "sessionID": permSessID,
        "action":    call.Name,
        "resources": []string{pattern},
        "metadata":  map[string]any{},
        "source": map[string]any{
            "type":      "tool",
            "messageID": messageID,
            "callID":    call.ID,
        },
    },
})
```

Add `"permission.v2.replied"` event in `handlePermissionReply` / `handleV2SessionPermissionReply`.

**Files:** `internal/server/agent_loop.go`, `internal/event/event.go`, permission reply handlers

---

## Gap 10 — `session.next.step.started` missing `model.variant` field  [S3]

### What the SDK defines

```typescript
export type EventSessionNextStepStarted = {
    ...
    properties: {
        ...
        model: { id: string; providerID: string; variant?: string; };
    };
};
```

### What Go currently emits

```go
type SessionNextStepStartedProps struct {
    ...
    Model struct {
        ID         string `json:"id"`
        ProviderID string `json:"providerID"`
        // ❌ missing variant (optional but expected by TUI model display)
    } `json:"model"`
}
```

### Fix

Add `Variant string \`json:"variant,omitempty"\`` to the Model struct in
`SessionNextStepStartedProps`.

**File:** `internal/event/event.go`

---

## Gap 11 — v1 `message.part.updated` event extra `time` field  [S3]

### What the SDK defines (v1)

```typescript
// dist/gen/types.gen.d.ts line 354
export type EventMessagePartUpdated = {
    type: "message.part.updated";
    properties: {
        part: Part;
        delta?: string;   // NO time field in v1
    };
};
```

### What v2 SDK defines

```typescript
// dist/v2/gen/types.gen.d.ts line 4221
// (no EventMessagePartDelta in global event list — it's a separate event)
// EventMessagePartUpdated has sessionID, part, time
```

### What Go currently emits

```go
type PartUpdatedProps struct {
    SessionID string `json:"sessionID"`
    Part      any    `json:"part"`
    Time      int64  `json:"time"`
}
```

The v1 event has no `time` or `sessionID` in properties — those are in the Part. The v2 event
does have them. Go's shape is v2-compatible but adds extra fields in v1 stream. Since v1 clients
ignore unknown fields, this is low risk. However, for full fidelity, the v1 stream should omit
`time`.

This is classified S3 (minor). No immediate fix required.

---

## Gap 12 — v2 `handleV2SessionMessages` missing `sessionID` in `mapToV2Info` call  [S3]

The `handleV2SessionGet` returns `map[string]any{"data": s.mapToV2Info(sess)}` where
`mapToV2Info` doesn't include a top-level `sessionID`. The SDK's `SessionV2Info` doesn't have
a `sessionID` field (it has `id`). This is fine, just noting it for completeness.

The `SessionV2Info` type doesn't have `slug`. Only the v1 `Session` type (and v2 `GlobalSession`)
has `slug`. So the existing `slug` on `session.Session` is only relevant for v1 stream events.

---

## Gap 13 — v1 `AssistantMessage.agent` missing  [S2]

Already partially documented in PLAN-delegate-task-fix.md (Bug 7), but also affects the v1
stream. The v1 `AssistantMessage` type does NOT have an `agent` field:

```typescript
// dist/gen/types.gen.d.ts line 98
export type AssistantMessage = {
    id: string; sessionID: string; role: "assistant";
    time: { created: number; completed?: number; };
    parentID: string; modelID: string; providerID: string; mode: string;
    path: { cwd: string; root: string; };  // REQUIRED in v1
    cost: number; tokens: {...}; finish?: string; ...
    // NO agent field in v1
};
```

The v2 `SessionMessageAssistant` (used in `/api/session/{id}/message` response) DOES have
`agent: string` (required). This v2 `agent` is already documented in Bug 7 of delegate plan.

The new finding: v1 `AssistantMessage` requires `path: { cwd, root }` (required, not optional)
and `mode: string`. Go's `Message` struct has `Path *MsgPath \`json:"path,omitempty"\`` and
`Mode string \`json:"mode,omitempty"\`` — these are pointer/omitempty so they may be null/absent
when the TUI expects them as required strings.

### Fix

When building the assistant message before generation, always set `Path` and `Mode` to
non-empty values. `Mode` should default to `"chat"` if not set. `Path.Cwd` should be
`s.workdir`, `Path.Root` should be `s.workdir`.

**File:** `internal/session/store.go` → `NewAssistantMessage`

---

## Gap 14 — v2 `mapToV2Message` missing `sessionID` / `metadata` on user messages  [S3]

The v2 `SessionMessageUser` type has no `sessionID` field at the message level but has optional
`metadata`. Go currently includes `sessionID` (extra, harmless) but omits `metadata`. Add
`metadata: map[string]any{}` to user message output in `mapToV2Message`.

---

## Summary table

| # | File | Issue | Severity |
|---|------|-------|----------|
| 1 | `session/session.go`, `session/store.go` | v1 `Session` missing `version` and `projectID` | S1 |
| 2 | `server/v2_handlers.go` → `mapToV2Message` | v2 messages use `role` not `type` discriminant | S1 |
| 3 | `server/v2_handlers.go` → `mapToV2Message` | v2 tool state wrong shape (missing `structured`, `content`; wrong `error` type) | S1 |
| 4 | `server/v2_handlers.go` → `mapToV2Info` | v2 session `model.id` = `providerID/modelID` (should be just `modelID`) | S2 |
| 5 | `server/v2_handlers.go` → `handleV2ModelList` | v2 model `cost` is flat object (should be array); missing `api`, `time`, `status` | S2 |
| 6 | `server/v2_handlers.go` → `handleV2ProviderList` | v2 provider missing required `api` and `request` fields | S2 |
| 7 | `server/v2_handlers.go` → `handleV2AgentList` | v2 agent missing required `request` field | S2 |
| 8 | `server/v2_handlers.go` → permission lists | v2 permission list emits `tool`/`type`/`title` (should be `action`/`resources`) | S1 |
| 9 | `server/agent_loop.go`, `event/event.go` | `permission.v2.asked` event never emitted | S1 |
| 10 | `event/event.go` | `session.next.step.started` missing optional `model.variant` | S3 |
| 11 | `event/event.go` | v1 `message.part.updated` has extra `time` field | S3 |
| 13 | `session/store.go` → `NewAssistantMessage` | v1 `AssistantMessage.path` and `mode` may be omitted | S2 |
| 14 | `server/v2_handlers.go` → `mapToV2Message` | User message missing `metadata: {}` | S3 |

---

## Implementation order

1. **Gap 9** — emit `permission.v2.asked` (standalone, low risk)
2. **Gap 1** — add `version` and `projectID` to `session.Session`
3. **Gap 2 + 3** — rewrite `mapToV2Message` (single function, highest TUI impact)
4. **Gap 4** — fix `model.id` in `mapToV2Info` (one line)
5. **Gap 8** — fix permission request list shape
6. **Gap 13** — ensure `path` and `mode` always populated on assistant messages
7. **Gap 5, 6, 7** — add stub `api`/`request`/`cost` fields to model/provider/agent responses
8. **Gap 10** — add `variant` to step-started event (trivial)
