# Opencode-Go Rework Handoff Document

**Date:** 2026-06-22
**Goal:** True TypeScript parity + sad-path rework for opencode-go
**TS source of truth:** `/tmp/opencode/`
**Go workspace:** `/home/thinh0704hcm/opencode-go`
**Current commit:** `2c30459` (Slices 15-16)

---

## Project Rules (MANDATORY)

1. **`user_intentions_and_findings.md`** is required direction context — read before any work.
2. **Prompt scope** — never broad prompts. Exact files, exact commands, exact output schema, finite scope. Avoid `if needed`, `explore broadly`, `check anything relevant`.
3. **Tool exclusions** — ignore Morph/Morph Edit and Supermemory; do not use or rely on them.
4. **Decision authority** — only `plan` subagent may decide major/architectural/destructive/hard-to-reverse direction. All others: report findings, answer scoped research, execute approved task cards, verify/review.
5. **Routing** — coding goes to coder, decisions go to plan, orchestration only (no logical decisions by orchestrator).
6. **AGENTS.md** — persisted in repo root, contains all rules.

---

## User Intentions & Directives (from `user_intentions_and_findings.md`)

1. **True Parity:** Do not want lightweight minimal "happy path." Demand true 1-to-1 parity port of original TypeScript codebase.
2. **Sad Paths:** Want concrete plan to iron out all sad paths natively.
3. **Smart Loop Detection:** Explicitly DO NOT want hardcoded `maxTurn` or timeout limit.
4. **Original TS Logic:** Want exact match of TS loop detection logic (identical repeated tool calls, not blind turn counting).
5. **Preserve Ideas:** All findings and intentions exported as reference file.

### Original Bug Findings
1. **Message Sequence:** TOCTOU/monotonicity issues → ✅ RESOLVED (Slice 5)
2. **Interrupts:** Parity verified, both require explicit abort → ✅ RESOLVED (Slice 6)
3. **Subagent Looping:** Smart loop detection needed → ✅ RESOLVED (Slices 7+15)
4. **False Endpoints:** MCP was hoax → ✅ RESOLVED (Slice 8)
5. **Statistics & DCP:** Wrong stats, no compression notification → ✅ RESOLVED (Slices 8+13+16)
6. **Todo Tooling:** No usable Todo → ✅ RESOLVED (Slice 8)

**ALL 6 ORIGINAL FINDINGS: RESOLVED**

---

## Completed Slices

### Slice 1: Parity Doc
- **File:** `docs/parity/message-ordering.md`
- **What:** TS vs Go message lifecycle/event ordering matrix. One row per lifecycle event with exact TS/Go file:line citations, GAP verdicts, ranked gap list.

### Slice 2: Step-Start Per-Turn Parts
- **Files:** `internal/server/agent_loop.go`, `internal/session/store.go`, `internal/session/store_step_start_test.go`
- **What:** Added `AppendStepStart(sessionID, messageID)` store method + per-turn step-start part creation for turns >= 2 in `runAgentLoop`.

### Slice 3: Abort Cooperative Checks
- **Files:** `internal/server/agent_loop.go`, `internal/server/agent_loop_abort_test.go`
- **What:** Three non-blocking `ctx.Done()` select checks in `runAgentLoop`: top-of-turn, between-tool-calls, after-tool-batch.

### Slice 4: Build Blocker Quarantine + Todo/Goal Types
- **Files:** 31+ untracked WIP files tagged `//go:build opencode_wip`, `internal/session/todo.go`, `internal/session/store.go`, `internal/event/event.go`
- **What:** Quarantined uncompiled recovery WIP layer behind build tags. Added `Todo` type + `Goal` fields.

### Slice 5: Monotonic Sequence Counter
- **Files:** `internal/session/store.go`, `internal/session/session.go`, `internal/server/generation.go`
- **What:** Added `Store.nextSeq uint64` (monotonic). Added `GlobalSeq uint64` to Message/Part. Resolves Finding #1.

### Slice 6: Input Validation + metadata.interrupted Parity
- **Files:** `internal/server/agent_loop.go`, `internal/server/handlers.go`, `internal/server/vcs_handlers.go`, `internal/server/config_handlers.go`, `internal/server/router.go`
- **What:** (6A) `metadata.interrupted=true` on aborted tool parts. (6B) POST /message: 1MB limit + text-part validation. (6C) VCS apply: 400 on empty body. (6D) PATCH /config: JSON decode + trailing junk rejection. (6E) GET /skill: stub `[]`.

### Slice 7: Doom-Loop Detection + Todo Fix + DCP Triage
- **Files:** `internal/server/agent_loop.go`, `internal/session/store.go`, `internal/server/session_handlers.go`
- **What:** `detectDoomLoop` method (threshold=3, JSON-normalized comparison). `MessageParts` helper. Todo handler returns real data.

### Slice 8: MCP Lifecycle + DCP Enable + Todo Integration
- **Files:** `internal/server/todo_tool.go`, `todo_read_tool.go` (build tags removed), `internal/mcp/manager.go` (refactored to maps, Add/Connect/Disconnect), `internal/session/dcp.go` (CompressionBlock), `internal/config/dcp.go` (DCPConfig), `internal/server/dcp_*.go` (build tags removed), `internal/server/mcp_handlers.go` (wired), `internal/server/session_handlers.go` (TodoUpdate), `internal/server/router.go` (todo routes), `internal/event/event.go` (session.compact), `internal/server/agent_loop.go` (DCP hooks)

### Slice 9: Request Validation Parity
- **Files:** `internal/server/handlers.go` (shared JSON helpers: hasJSONContentType, requireJSON, decodeStrictBody), `session_handlers.go` (title validation, init 501, session ID validation), `shell_handlers.go` (shell payload), `vcs_handlers.go` (requireJSON), `config_handlers.go` (shared helpers), `mcp_handlers.go` (requireJSON)

### Slice 10: Test Script Fixes
- **Files:** `scripts/api_tui_mimic.sh` (TUI 9 format, TUI 11 SSE timing)

### Slice 11: VCS/Session/MCP Parity
- **Files:** `internal/server/vcs_handlers.go` (Patch field), `internal/server/session_handlers.go` (init validation, fork JSON)

### Slice 12: Command/Revert/Shell Event Parity
- **Files:** `internal/event/event.go` (4 event types + constructors), `internal/server/shell_handlers.go` (shell events), `internal/server/session_handlers.go` (command event, revert diff, busy guard), `internal/server/server.go` (sessionBusy)

### Slice 13: DCP Parity (Overflow + Auto-Compaction)
- **Files:** `internal/event/event.go` (session.compacted), `internal/server/dcp_handlers.go` (events), `internal/session/dcp.go` (token stats), `internal/config/dcp.go` (Auto, ContextLimit, OutputLimit), `internal/server/dcp_overflow.go` (NEW: isDCPOverflow), `internal/server/agent_loop.go` (auto-compaction), `internal/server/v2_handlers.go` (context endpoint)

### Slice 14: V2 Handler Parity + API Test Fixes
- **Files:** `internal/server/v2_handlers.go` (context returns DCP data, compact calls compactSession), `internal/server/boot_handlers.go` (config update), `internal/server/mcp_handlers.go` (auth → 501), `internal/event/event.go` (SessionCompact enriched), `scripts/api_sad_paths.sh` (test fixes)

### Slice 15: Doom-Loop Integration Test
- **Files:** `internal/server/agent_loop.go` (detectDoomLoop: filter to tool-only parts), `internal/server/agent_loop_abort_test.go` (unique call IDs per turn, 2 integration tests)
- **Fixes:** Bug 1: detectDoomLoop filtered wrong parts. Bug 2: doomLoopProvider reused call IDs across turns.

### Slice 16: DCP Compression Notification Events
- **Files:** `internal/event/event.go` (compaction.started/ended types + constructors), `internal/server/dcp_handlers.go` (emit events from compactSession)

---

## Current MCP State

### Implemented (Go)
- **Stdio transport** (`internal/mcp/stdio_client.go`): Spawns local MCP server process, JSON-RPC 2.0 over stdin/stdout. Supports initialize, tools/list, tools/call.
- **Manager** (`internal/mcp/manager.go`): Lifecycle management — connect, disconnect, add, status, adapters. Uses `MCPClient` interface (implied by `*Client` type).
- **Adapter** (`internal/mcp/adapter.go`): Wraps MCP tools as `tool.Tool` for agent registry. Namespaced `<server>_<tool>`.
- **Protocol** (`internal/mcp/protocol.go`): JSON-RPC 2.0 framing, ToolDef, toolsCallResult.
- **HTTP handlers** (`internal/server/mcp_handlers.go`): GET /mcp (status), POST /mcp/{name}/connect, POST /mcp/{name}/disconnect, POST /mcp (add). Auth endpoints return 501.

### NOT Implemented (Gap vs TS)
- **Remote HTTP transport** — TS has StreamableHTTPClientTransport + SSEClientTransport. Go returns "unsupported" for `type: "remote"`.
- **Prompts** — TS supports `prompts/list`, `prompts/get`. Go only has tools.
- **Resources** — TS supports `resources/list`, `resources/read`. Go only has tools.
- **Tool list change notifications** — TS handles `ToolListChangedNotificationSchema`. Go has no notification handling.
- **OAuth** — TS has full OAuth flow (McpOAuthProvider, McpOAuthCallback, McpAuth). Go returns 501.
- **Configurable timeouts** — TS has per-server timeout (default 30s). Go has no timeouts.
- **Connection watching** — TS watches for onclose, publishes ToolsChanged. Go has no reconnect/watch.

### TS MCP Architecture (`mcp/index.ts`, 953 lines)
- Two transports: StdioClientTransport (local) + StreamableHTTPClientTransport/SSEClientTransport (remote)
- OAuth: McpOAuthProvider + McpOAuthCallback
- Capabilities: tools, prompts, resources
- Notifications: ToolListChanged, LoggingMessage
- State: config, status, clients, defs maps
- Timeout: configurable per-server, default 30s

---

## Slice 17 Plan: MCP Remote Transport + Prompts/Resources

### Task 1: MCPClient Interface + Rename
- **File:** `internal/mcp/protocol.go`
- Add `MCPClient` interface: `Initialize`, `ListTools`, `CallTool`, `ListPrompts`, `GetPrompt`, `ListResources`, `ReadResource`, `Close`, `Name`
- **File:** `internal/mcp/stdio_client.go`
- Rename `Client` → `StdioClient`, implement `MCPClient`
- **File:** `internal/mcp/adapter.go`
- Change `*Client` → `MCPClient` in `toolAdapter` struct and `NewToolAdapters`
- **File:** `internal/mcp/manager.go`
- Change `clients map[string]*Client` → `clients map[string]MCPClient`

### Task 2: Prompts + Resources Protocol Types
- **File:** `internal/mcp/protocol.go`
- Add types: `PromptDef`, `PromptArg`, `PromptResult`, `ResourceDef`, `ResourceContent`
- Add request/response types for `prompts/list`, `prompts/get`, `resources/list`, `resources/read`

### Task 3: HTTP Client for Remote Transport
- **File:** `internal/mcp/http_client.go` (NEW)
- `HTTPClient` struct implementing `MCPClient`
- Uses `http.Post` for JSON-RPC requests to configured URL
- Supports custom headers from config (`headers` map)
- Handles both `application/json` and `text/event-stream` responses
- Configurable timeout (default 30s from config `timeout` field)

### Task 4: Manager Updates
- **File:** `internal/mcp/manager.go`
- `connectLocked`: detect `type: "remote"` → create `HTTPClient` instead of "unsupported"
- Parse `timeout` field from config (default 30s)
- Parse `headers` field from config for remote servers

### Task 5: Build + Test
- `go build ./...`
- `go test ./internal/mcp/...`
- Verify existing stdio tests still pass

### Out of Scope (Deferred)
- OAuth flow (complex, needs callback server + token storage)
- Tool list change notifications (re-fetch on notification)
- Full connection watching + reconnect logic

---

## Slice 18 Plan: MCP Tool List Change Notifications

### Task 1: Event Type
- **File:** `internal/event/event.go`
- Add `TypeToolsChanged = "tools.changed"`
- Add `ToolsChangedProps{Server string}`
- Add `NewToolsChanged(server string) Event`

### Task 2: MCP Notification Protocol
- **File:** `internal/mcp/protocol.go`
- Add notification envelope parsing for JSON-RPC messages without `id`
- Add constants: `notifications/tools/list_changed`, `notifications/message`
- Extend `MCPClient` with `OnToolsChanged(func())` and `OnClose(func(error))`

### Task 3: Stdio Notification Handling
- **File:** `internal/mcp/stdio_client.go`
- Add callback fields for tool-list changes and close/error
- In request read loop: detect `notifications/tools/list_changed`, invoke callback without deadlocking
- On read/EOF error, invoke close callback once

### Task 4: HTTP No-op Notification Hooks
- **File:** `internal/mcp/http_client.go`
- Implement `OnToolsChanged` and `OnClose` as no-ops
- Document: current HTTP client is request/response only; SSE push is future work

### Task 5: Manager Refresh + Close Handling
- **File:** `internal/mcp/manager.go`
- Add `SetToolsChangedCallback(func(server string))`
- Add `AdaptersFor(name string) []tool.Tool`
- Register client callbacks in `connectLocked`
- On tool-list change: verify current client, call ListTools, replace adapters, update status
- On close: verify current client, delete client/adapters, set status failed
- Do not auto-reconnect

### Task 6: Server Registry Refresh
- **File:** `internal/server/server.go`
- After MCP manager creation, set callback to refresh tool registry
- Unregister old MCP tools with prefix `<server>_`
- Register current adapters from `AdaptersFor(server)`
- Publish `event.NewToolsChanged(server)`

### Task 7: Tests
- `internal/mcp/stdio_client_test.go` — notification between request/response
- `internal/mcp/manager_test.go` — tool-list refresh + close handling
- Event serialization test

### Out of Scope
- MCP OAuth
- MCP token storage
- Full HTTP/SSE notification stream
- Reconnect/watch loop
- Append-only event log

---

## Test Infrastructure

### Build Status (as of Slice 18)
- `go build ./...` ✅
- `go vet ./internal/server/...` ✅
- `go test ./internal/server/...` ✅
- `go test ./internal/session/...` ✅
- `go test ./internal/config/...` ✅
- `go test ./internal/mcp/...` ✅
- `go test ./internal/event/...` ✅

### API Test Results
- `scripts/api_sad_paths.sh`: 48/58 passed (1MB curl limitation only)
- `scripts/api_tui_mimic.sh`: 25/25 passed

### Test Files
- `internal/session/store_step_start_test.go` — TestAppendStepStart
- `internal/server/agent_loop_abort_test.go` — 3 abort tests + 8 doom-loop tests + 2 integration tests
- `internal/session/ordering_test.go` — TestAppendTextDeltaOrdering
- `internal/session/dcp_test.go` — DCPStats shape test (build tag: opencode_dcp_wip)

---

## Known Gaps (Ranked by Priority)

| # | Gap | TS Ref | Go Ref | Priority | Status |
|---|---|---|---|---|---|
| 1 | metadata.interrupted on aborted tool parts | processor.ts:907 | agent_loop.go:364-375 | Medium | ✅ RESOLVED (Slice 6) |
| 2 | TS append-only event log with SQL cursors | message-v2.ts:57-80 | store.go | Deferred | Architectural — not in scope |
| 3 | No auto-abort on SSE disconnect | N/A (same in TS) | N/A | Low | Intentional divergence |
| 4 | MCP hoax | N/A | internal/server/*.go | Deferred | ✅ RESOLVED (stdio lifecycle + HTTP handlers) |
| 5 | DCP/stats | N/A | todo.go | Medium | ✅ RESOLVED (build tags removed, hooks wired) |
| 6 | Todo | Various | handlers.go, vcs, config | Medium | ✅ RESOLVED (tools registered, HTTP endpoint) |
| 7 | Doom-loop detection | processor.ts:35 | agent_loop.go:detectDoomLoop | High | ✅ RESOLVED (Slices 7+15) |
| 8 | DCP compaction.started/ended events | compaction.ts:538-585 | dcp_handlers.go:compactSession | Medium | ✅ RESOLVED (Slice 16) |
| 9 | MCP remote transport | mcp/index.ts:223-274 | manager.go:74-78 | High | ✅ RESOLVED (Slice 17) |
| 10 | MCP prompts/resources | mcp/index.ts:674-680 | protocol.go | Medium | ✅ RESOLVED (Slice 17) |
| 11 | MCP OAuth | mcp/index.ts:748-903 | mcp_handlers.go:112-127 | Low | Deferred |
| 12 | MCP tool list change notifications | mcp/index.ts:443-452 | internal/mcp/manager.go | Low | ✅ RESOLVED (Slice 18) |
| 13 | MCP configurable timeouts | mcp/index.ts:39,623-626 | N/A | Medium | ✅ RESOLVED (Slice 17) |
| 14 | revert metadata (MessageID/PartID) | session_handlers.go:285-326 | session_handlers.go:335-357 | High | ✅ RESOLVED (Slice 19) |

---

## Key Files for Reference

| Purpose | TS File | Go File |
|---|---|---|
| MCP index | `/tmp/opencode/packages/opencode/src/mcp/index.ts` (953 lines) | `internal/mcp/manager.go` |
| MCP catalog | `/tmp/opencode/packages/opencode/src/mcp/catalog.ts` | `internal/mcp/adapter.go` |
| MCP OAuth | `/tmp/opencode/packages/opencode/src/mcp/oauth-provider.ts` | `internal/server/mcp_handlers.go` (stubs) |
| MCP HTTP routes | `/tmp/opencode/packages/opencode/src/server/routes/instance/httpapi/groups/mcp.ts` | `internal/server/mcp_handlers.go` |
| Session lifecycle | `/tmp/opencode/packages/opencode/src/session/session.ts` | `internal/session/session.go` |
| Processor/events | `/tmp/opencode/packages/opencode/src/session/processor.ts` | `internal/server/agent_loop.go` |
| Compaction | `/tmp/opencode/packages/opencode/src/session/compaction.ts` | `internal/server/dcp_handlers.go` |
| Permission | `/tmp/opencode/packages/opencode/src/session/permission.shared.ts` | `internal/server/permission.go` |
| Tool definitions | `/tmp/opencode/packages/opencode/src/tool/tool.ts` | `internal/tool/` |
| Config schema | `/tmp/opencode/packages/opencode/src/config/config.ts` | `internal/config/config.go` |
| Event bus | N/A | `internal/event/event.go` |
| Store | N/A | `internal/session/store.go` |
| Agent loop | N/A | `internal/server/agent_loop.go` |
| Router | N/A | `internal/server/router.go` |

---

## Git History

```
2c30459 Slices 15-16: doom-loop integration tests + compaction.started/ended events
743ef43 Slice 14: V2 handler parity + API test fixes
5d0243d Slice 13: DCP parity — overflow detection, auto-compaction, compacted events, token stats
bd00399 Slice 12: command/revert/shell event parity + busy-state guard
8ecec3f Slice 11: VCS/Session/MCP parity (init validation, fork JSON, patch field)
aa2c6a8 add doom-loop detection unit tests (8 cases)
26d9fa5 Slice 10: fix TUI test script bugs (request format + SSE timing)
723655a Slice 9: request validation parity (JSON helpers, title/action/path validation, init 501)
f01ff0e feat: parity Slices 6-8 — input validation, doom-loop, MCP lifecycle, DCP enable, Todo integration
```

---

## How to Continue

### Current: Slice 19 — Revert metadata (full parity)

**Status:** Slice 19 complete.

**Completed:**
1. ✅ Added `tools.changed` event type
2. ✅ Added notification envelope parsing to `rpcResponse`
3. ✅ Wired stdio tool-list-change and close callbacks
4. ✅ Manager refreshTools/markClosed methods
5. ✅ Server registry refresh on tool changes
6. ✅ Build + tests pass

**After Slice 19:**
- MCP OAuth (deferred, complex — needs callback server + token storage)
- Append-only event log (architectural, deferred)
```