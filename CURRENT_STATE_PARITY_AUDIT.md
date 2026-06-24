# Current-State Parity Audit

## Baseline

- Git state at audit time: dirty before and after audit, unchanged by audit. `45` modified or staged, `18` untracked.
- Last commits:
  - `ea0f2b7 Bug hunt: fix 18 confirmed bugs from 19-finding audit`
  - `2c30459 Slices 15-16: doom-loop integration tests + compaction.started/ended events`
  - `743ef43 Slice 14: V2 handler parity + API test fixes`
  - `5d0243d Slice 13: DCP parity - overflow detection, auto-compaction, compacted events, token stats`
  - `bd00399 Slice 12: command/revert/shell event parity + busy-state guard`
- Test: `go test ./internal/server ./internal/event ./internal/session -count=1 -timeout 150s` passed.
  - `internal/server 115.786s`
  - `internal/event 0.005s`
  - `internal/session 0.016s`
- Evidence used:
  - `user_intentions_and_findings.md`
  - `user_findings_and_intentions.md`
  - current Go code under `/home/thinh0704hcm/opencode-go`
  - upstream official docs: `https://opencode.ai/docs/`, `https://opencode.ai/docs/mcp-servers/`, `https://opencode.ai/docs/plugins/`, `https://opencode.ai/docs/providers/`
  - upstream source: `https://github.com/anomalyco/opencode` at `fc95a84b42f909e4202a070978fc939e4be7a6ee`
- Ignored as evidence: `docs/**`, `PLAN-*.md`, `fix_todos.md`, `notification-fix.md`, `slice6-input-validation.md`, existing parity/planning docs.
- `/tmp/opencode` is empty; exact old TS parity evidence is incomplete where noted.

## Findings

| ID | Severity | Area | Current Evidence | Upstream/Intent Target | Risk | Fix Direction |
|---|---|---|---|---|---|---|
| F1 | P1 | Message/event sequencing | `internal/session/store.go:17,266-333` uses in-memory `nextSeq`; user message and all parts share one `GlobalSeq`; `internal/session/persist.go:12-15` persists no seq cursor. | Upstream durable seq/projection: `packages/core/src/session/input.ts:80`, `projector.ts:203`, migration `20260603040000_session_message_projection_order.ts`; user says sequence causes TOCTOU. | Restart/concurrency breaks monotonic ordering; clients paginate/project stale order. | Persist sequence cursor; assign unique monotonic seq per durable event/part; restore on load. |
| F2 | P1 | Abort/delegated children | `internal/server/session_handlers.go:261-283` aborts only requested session; `server.go:155-180` cancels one key; `delegate_tools.go:111-156` child uses independent `context.Background()`. | Upstream interrupt service: `packages/core/src/session.ts:388-389`, `session/run-coordinator.ts:92-101`; user scope says delegated child sessions. | Parent abort leaves child agent running. | Parent abort must recursively cancel active child sessions from `store.Children`. |
| F3 | P1 | Subagent/delegate loop detection | `internal/server/agent_loop.go:20-22` fixed threshold; `162-197` checks only current assistant parts; `276-303` hardcoded `90s` stream timeout plus `3` retries. | User forbids hardcoded `maxTurn`/timeout loop stop. Upstream current says repeated identical tool-call bound is TODO: `packages/core/src/session/runner/llm.ts:50-51,85-86`. Evidence incomplete for old TS `/tmp/opencode`. | Infinite loops still possible; false stops tied to transport limits. | Implement canonical repeated-tool-call detection across turns/subagents; keep transport timeout separate from loop policy. |
| F4 | P1 | MCP/plugin reality | MCP adapter names `server_tool`: `internal/mcp/adapter.go:33`; disconnect unregisters `server:`: `internal/server/mcp_handlers.go:58`; boot gated by `OPENCODE_GO_MCP`: `server.go:227-228`; stdio prompts/resources unsupported, OAuth unsupported: `mcp_handlers.go:125-144`. | Upstream MCP config supports local/remote/OAuth: `packages/core/src/config/mcp.ts`; plugin boot loads provider/external plugins: `packages/core/src/plugin/internal.ts:97-112`; docs above. | Stale tools remain after disconnect; configured MCP/plugin surface appears connected but incomplete. | Fix unregister prefix; honor config-enabled servers; implement or explicitly unsupported MCP subfeatures with typed errors. |
| F5 | P1 | DCP/context/events | `/api/session/{id}/context` returns only blocks/stats: `internal/server/v2_handlers.go:1155-1164`; `compactSession` start can return without end: `dcp_handlers.go:24-68`; v2 compact double-publishes: `v2_handlers.go:1176-1183`. | Upstream context handler returns `session.context`: `packages/server/src/handlers/session.ts:212`; compaction events are `session.next.compaction.started/delta/ended`: `packages/core/src/session/event.ts:401-422`. | UI sees wrong context and dangling/duplicate compaction events. | Return active context messages; make compaction start/end balanced; remove duplicate publish; align event names/schema. |
| F6 | P2 | Token accounting | `internal/provider/provider.go:103-108` only input/output/total; `internal/provider/openai.go:80-83` parses no details; `internal/session/store.go:633-645` zeros reasoning/cache. | Upstream tracks reasoning/cache: `packages/core/src/session/projector.ts:66-68,102-104`; OpenAI converters parse cached/reasoning tokens: `openai-compatible-chat-language-model.ts:284-290`. | Cost/stat UI undercounts reasoning/cache; DCP budgets wrong. | Extend usage struct, parse OpenAI details, persist real reasoning/cache counts. |
| F7 | P2 | Todo persistence | Todo tool/endpoints exist, but `internal/session/persist.go:12-15` persists only session/messages; `store_todo.go:17-27` calls persist but file drops todos. | Upstream has persisted `TodoTable`: `packages/core/src/session/sql.ts:99-114`; tests require ordered persistence: `packages/core/test/session-todo.test.ts:53-78`. | Todos vanish after restart. | Add todos to persisted session JSON and load path. |
| F8 | P2 | Provider/model variants/config | `cmd/opencode-go/main.go:180-203` discards provider ID and headers in `NewOpenAI("openai", ...)`; `internal/provider/openai_headers.go:18` ignores passed headers; `v2_handlers.go:809-815` emits zero cost and empty variants. | Upstream model info has request/variants/cost: `packages/core/src/model.ts:57-73`; config tests verify merge: `packages/core/test/config/provider.test.ts:253-267`. | Wrong model catalog; variant prompts/config headers ignored. | Preserve provider ID/headers; expose configured cost/request/variants; apply selected variant body/headers. |
| F9 | P2 | Server/API compatibility stubs | Silent OK/empty routes: `internal/server/router.go:76-96,157-164,185-202,227-236`; `v2_handlers.go:1098-1108`. | Upstream API has typed groups for permission/question/project-copy/etc: `packages/server/src/api.ts:7-41`. | Clients believe actions succeeded when no state changed. | Replace exact silent stubs with real behavior or typed unsupported errors. |
| F10 | P3 | LSP/formatter | `internal/server/boot_handlers.go:308-315` returns `[]` for `/formatter` and `/lsp`. | Upstream config parses formatter/LSP: `packages/core/test/config/config.test.ts:292-380`; runtime integration is upstream TODO: `tool/write.ts:40`, `tool/edit.ts:84`. | Low parity drift; UI cannot show configured disabled/enabled state. | Return config-derived status; do not claim runtime formatting until implemented. |

## Task Cards

### F1 - Message/Event Sequencing

- Goal: Durable, strictly monotonic message/part ordering across restart.
- Files: `internal/session/store.go`, `internal/session/persist.go`, `internal/session/ordering_test.go`, `internal/session/persist_test.go`, `internal/server/v2_handlers_test.go`.
- Commands:
  - `go test ./internal/session -run 'TestOrdering|TestPersist' -count=1`
  - `go test ./internal/server -run 'TestV2.*Message|TestPromptAsync' -count=1`
  - `go test ./internal/server ./internal/event ./internal/session -count=1 -timeout 150s`
- Implementation Rules: persist `nextSeq`; restore max persisted seq on load; no shared `GlobalSeq` across separate emitted entities; preserve old JSON compatibility.
- Acceptance Tests: restart then append produces higher seq; user message parts have deterministic monotonic seq; existing v2 message responses unchanged except corrected seq.

### F2 - Parent Abort Cascades To Delegates

- Goal: `POST /session/{id}/abort` stops parent plus active child sessions.
- Files: `internal/server/session_handlers.go`, `internal/server/server.go`, `internal/server/delegate_tools.go`, `internal/server/delegate_abort_test.go`.
- Commands:
  - `go test ./internal/server -run 'TestDelegateAbortPropagation|TestSessionAbort|TestAgentLoopAbort' -count=1`
  - `go test ./internal/server ./internal/event ./internal/session -count=1 -timeout 150s`
- Implementation Rules: recursively walk `store.Children(id)`; call `cancelSession` for each active child; preserve "TUI disconnect does not auto-abort"; publish idle once per stopped session.
- Acceptance Tests: parent abort stops blocking child provider; direct child abort still passes; idle events emitted for parent and child.

### F3 - Smart Loop Detection

- Goal: Stop repeated identical tool-call loops without fixed turn/timeout policy.
- Files: `internal/server/agent_loop.go`, `internal/server/agent_loop_abort_test.go`, `internal/server/delegate_tools.go`, `internal/server/delegate_test.go`.
- Commands:
  - `go test ./internal/server -run 'TestDoomLoop|TestAgentLoop.*Doom|TestDelegate.*Loop' -count=1`
  - `go test ./internal/server ./internal/event ./internal/session -count=1 -timeout 150s`
- Implementation Rules: canonicalize tool JSON args; detect repeats across assistant/tool-turn boundary; scope by session + agent; transport timeout may remain provider/network-only, never loop policy.
- Acceptance Tests: identical repeated calls stop; reordered JSON args match; different tool/input/session/agent does not stop.

### F4 - MCP/Plugin Reality

- Goal: MCP connect/disconnect and configured plugin/MCP state truthful.
- Files: `internal/server/mcp_handlers.go`, `internal/server/server.go`, `internal/mcp/adapter.go`, `internal/mcp/stdio_client.go`, `internal/mcp/http_client.go`, `internal/mcp/manager.go`, `internal/mcp/*_test.go`.
- Commands:
  - `go test ./internal/mcp ./internal/server -run 'Test.*MCP.*|TestMCP.*' -count=1`
  - `go test ./internal/server ./internal/event ./internal/session -count=1 -timeout 150s`
- Implementation Rules: unregister prefix must match `server_`; honor config `enabled`; remove env-only gate or make it an explicit config override; unsupported OAuth/prompts/resources return typed error, not fake success.
- Acceptance Tests: disconnect removes registered tools; disabled MCP server stays disconnected; unsupported remote OAuth returns expected error envelope.

### F5 - DCP Context/Events

- Goal: Context endpoint and compaction events match upstream semantics.
- Files: `internal/server/v2_handlers.go`, `internal/server/dcp_handlers.go`, `internal/event/event.go`, `internal/event/v2_event_test.go`, `internal/server/dcp_*test.go`, `internal/session/dcp_test.go`.
- Commands:
  - `go test ./internal/server -run 'Test.*Compact|Test.*Context|TestDCP' -count=1`
  - `go test ./internal/event ./internal/session -run 'Test.*Compaction|TestDCP' -count=1`
  - `go test ./internal/server ./internal/event ./internal/session -count=1 -timeout 150s`
- Implementation Rules: `/api/session/{id}/context` returns active context messages; every started event has ended/failure; v2 compact publishes once; event type names align to `session.next.compaction.*`.
- Acceptance Tests: compact with no compressible messages has balanced terminal event; v2 compact emits one compact sequence; context excludes compacted history.

### F6 - Token Accounting

- Goal: Store real input/output/reasoning/cache usage.
- Files: `internal/provider/provider.go`, `internal/provider/openai.go`, `internal/session/store.go`, `internal/provider/openai_stream_test.go`, `internal/session/store_tool_test.go`.
- Commands:
  - `go test ./internal/provider -run 'TestOpenAI.*Usage|Test.*Stream' -count=1`
  - `go test ./internal/session -run 'Test.*Usage|Test.*Tokens' -count=1`
  - `go test ./internal/server ./internal/event ./internal/session -count=1 -timeout 150s`
- Implementation Rules: add reasoning/cache fields to usage; parse `prompt_tokens_details.cached_tokens`, `completion_tokens_details.reasoning_tokens`, Responses API equivalents; never overwrite absent details with bogus nonzero values.
- Acceptance Tests: fixture stream records cache read and reasoning; old responses still produce zero details.

### F7 - Todo Persistence

- Goal: Todos survive restart.
- Files: `internal/session/persist.go`, `internal/session/store_todo.go`, `internal/session/persist_test.go`, `internal/server/todo_endpoint_test.go`.
- Commands:
  - `go test ./internal/session -run 'TestPersist.*Todo|TestStoreTodo' -count=1`
  - `go test ./internal/server -run 'TestTodo' -count=1`
  - `go test ./internal/server ./internal/event ./internal/session -count=1 -timeout 150s`
- Implementation Rules: add `Todos []Todo` to per-session persisted JSON; load into `s.todos`; keep old files valid when `todos` absent.
- Acceptance Tests: write todos, reload store, GET endpoints return same ordered todos.

### F8 - Provider/Model Config

- Goal: Respect provider IDs, headers, costs, variants.
- Files: `cmd/opencode-go/main.go`, `internal/provider/resolve.go`, `internal/provider/openai_headers.go`, `internal/provider/registry.go`, `internal/server/v2_handlers.go`, `internal/provider/*test.go`, `internal/server/v2_handlers_test.go`.
- Commands:
  - `go test ./internal/provider -run 'TestResolveDefault|TestBuildRegistry|TestOpenAIHeaders' -count=1`
  - `go test ./internal/server -run 'TestV2.*Model|TestV2.*Provider' -count=1`
  - `go test ./internal/server ./internal/event ./internal/session -count=1 -timeout 150s`
- Implementation Rules: pass resolved `providerID`; apply configured headers on HTTP requests; expose model `cost`, `request`, `variants`; no hardcoded zero/empty when config provides values.
- Acceptance Tests: configured header reaches request; model list returns configured cost/variant; default model with slash parses once.

### F9 - Silent API Stubs

- Goal: No route returns fake success for unimplemented state changes.
- Files: `internal/server/router.go`, `internal/server/v2_handlers.go`, `internal/server/tui_handlers.go`, `internal/server/boot_handlers.go`, `internal/server/tui_conformance_test.go`, `internal/server/v2_handlers_test.go`.
- Commands:
  - `go test ./internal/server -run 'TestTUI|TestV2Permission|TestQuestion|TestSync|TestExperimental' -count=1`
  - `go test ./internal/server ./internal/event ./internal/session -count=1 -timeout 150s`
- Implementation Rules: exact stub set: `/sync/*`, `/question*`, `/api/permission/saved*`, `/tui/*`, `/experimental/*copy*`, `/experimental/worktree*`; implement real backing state or return typed unsupported error with non-2xx.
- Acceptance Tests: no listed mutating stub returns `200 true` without state mutation; read stubs expose real stored state or typed unsupported.

### F10 - LSP/Formatter Status

- Goal: Boot endpoints report configured state accurately.
- Files: `internal/server/boot_handlers.go`, `internal/config/config.go`, `internal/server/boot_conformance_test.go`.
- Commands:
  - `go test ./internal/server -run 'TestBootFormatter|TestBootLSP|TestBootConformance' -count=1`
  - `go test ./internal/server ./internal/event ./internal/session -count=1 -timeout 150s`
- Implementation Rules: parse config-defined formatter/LSP entries; return disabled/configured status; do not claim runtime formatting/LSP process support.
- Acceptance Tests: configured custom LSP appears; disabled formatter appears disabled; empty config still returns `[]`.

## Docs Cleanup

Deleted in cleanup:

- `docs/PARITY-audit-sad-happy-paths.md`
- `docs/parity-tasks/`
- `PLAN-true-parity-sad-paths.md`
- `fix_todos.md`
- `notification-fix.md`
- `slice6-input-validation.md`

Preserved:

- `user_intentions_and_findings.md`
- `user_findings_and_intentions.md`
