# Handoff: opencode-go Wire-Protocol Parity Remediation

**Audience:** the next agent continuing this work. Read this fully before touching code.
**Last updated:** 2026-06-24. Approved plan: `~/.claude/plans/streamed-giggling-yao.md`.

---

## 0. Ground rules (read first)

1. **DO NOT TRUST the markdown docs in this repo.** `CURRENT_STATE_PARITY_AUDIT.md`,
   `user_intentions_and_findings.md`, `user_findings_and_intentions.md`, and any `PLAN-*.md`
   are **outdated and partly false**. Verify every claim against actual code. This handoff and
   the approved plan supersede them.
2. **This is a wire-protocol parity problem, not a logic rewrite.** `opencode-go` is a Go
   reimplementation of the opencode **server**; it backs the **original TypeScript TUI / tg-bot**
   over HTTP+SSE (see `README.md:37-49`). Parity = the Go server speaks the exact protocol the
   real TS client expects. The Go port **already emits the full `session.next.*` protocol AND
   v1 `message.*`** — so most "rewrite" fears in the old docs are wrong.
3. **DO NOT build a `maxTurn`/timeout loop limiter, and do not chase "TS smart loop detection."**
   That feature **does not exist in upstream** (it's an explicit TODO in `runner/llm.ts`). Go's
   existing 3-identical-call doom-loop detector is already *more* than upstream. Leave it.
4. **Verify behavioral claims against a RUNNING comparison when possible**, else against upstream
   source. Prefer evidence over the user's framing — the user's distress was real but several of
   their original "findings" were false (MCP, todos, token stats all actually work).

---

## 1. Reference source locations

| What | Path | Notes |
|---|---|---|
| Go port (this repo) | `/home/thinh0704hcm/opencode-go/` | module `github.com/opencode-go/opencode-go` |
| **Upstream parity target** | `/tmp/sst-opencode/` | `sst/opencode` **v1.17.9**, commit `3cdd431`. Cloned fresh. THE authority. |
| Secondary upstream snapshot | `/tmp/anomalyco-opencode/` | `anomalyco/opencode` @ `fc95a84`. Similar protocol; use sst as primary. |
| `/tmp/opencode` | (empty) | The path the old intent file names — ignore, it's empty. |

The Go port hard-codes `Version = "1.16.0"` (`internal/server/server.go:26`); the target is 1.17.9.
Upstream is a heavy SST/Solid/native-deps monorepo — **running the live TS server is a yak-shave**
(bun install + `fix-node-pty` postinstall). Deriving contract from source is the pragmatic path.

---

## 2. Architecture (the lens that explains everything)

The user runs the **real TS TUI against the Go server**. Their symptoms (subagents loop forever,
wrong msg order, DCP stats wrong, "feels off") are all **protocol mismatches** between what the Go
server emits and what the TUI expects. When debugging "X breaks in the TUI", find **what HTTP
request / SSE event the TUI actually uses** in `/tmp/sst-opencode/packages/tui/src` and
`/tmp/sst-opencode/packages/sdk/js/src`, then check the Go server matches. This is how the
`/compact` bug was solved (see §4).

SSE envelope upstream: `{id, type, properties}`, first event `server.connected`. Go matches this.

---

## 3. Verified findings (corrected — the TRUE state)

| ID | Status | Reality (verified) |
|---|---|---|
| **D1** subagent delegation | ✅ FIXED | Was inverted: detached async on `context.Background()`, returned "read later" referencing a nonexistent `delegation_read` tool → guaranteed loop. Upstream `task` is foreground-synchronous by default. |
| **D3** compaction events | ✅ FIXED | Go emitted non-namespaced `compaction.*` + `session.compact/compacted`; upstream wants `session.next.compaction.started/delta/ended` w/ `messageID`+`reason`. |
| **/compact break** | ✅ FIXED | TUI's `/compact` → `POST /session/{id}/summarize`, but the Go handler did NO compaction (just title refresh) → TUI hung. Also `/tui/execute-command` 501'd everything. |
| **D2** msg/part ordering | ✅ ADDRESSED | Felt "wrong order" was the async-delegation race → fixed by D1, locked with `TestDelegateChildEventOrdering`. Happy-path ordering already covered (`TestTUIPrompt_EventSequence`, `TestMonotonicGlobalSeq`). Per-part `GlobalSeq` vs upstream per-message `seq` divergence remains but is **NOT a confirmed bug** — do not rewrite without a real TUI reproduction. |
| **D4** 1.17.9 event family | ✅ ALREADY PRESENT | Go already emits full `session.next.*` + v1 `message.*`; upstream 1.17.9 still serves v1 too. No rebase needed. |
| **D5** silent stubs | ✅ ASSESSED (no blind change) | `/formatter`,`/lsp` already correct — Go runs neither, returns `[]`, TUI handles it; fabricating status would mislead. `/sync/history`→`[]` and `/experimental/*`→`200 true` are low-value, risky to change blind (TUI may call on boot). Deferred pending real-TUI verification. |
| **D6** minor | ✅ ASSESSED, no change | Verified wire types: Todo = `{content,status,priority}` (Go matches; `position` is internal-only upstream, not on wire). Tokens = `{input,output,reasoning,cache:{read,write}}` (Go matches; `nonCachedInputTokens` is internal-only). Adding either would be a non-upstream field nothing reads. |
| Stage 0 harness | ⬜ PARTIAL | Upstream 1.17.9 wire contract extracted (see §6). Go-side golden-replay harness NOT built. |
| ~~MCP / todos / token stats~~ | ❌ NOT BUGS | Old docs claimed these broken. **They work.** Do not "fix" them. |
| ~~loop detection / maxTurn~~ | ❌ NOT A BUG | See ground rule #3. |

---

## 4. Completed work this session (with evidence)

### D1 — synchronous subagent delegation
- `internal/server/delegate_tools.go`: `runDelegated` now runs the child agent loop to completion
  **foreground (default)** on a **parent-derived context** (`context.WithCancel(ctx)`), returns the
  child's real result as `<task id=… state="completed"><task_result>…</task_result></task>`
  (mirrors upstream `tool/task.ts renderOutput`). Background mode is opt-in:
  `background=true` AND env `OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS=true`.
  New helpers: `assistantText`, `renderTaskResult`, `backgroundSubagentsEnabled`.
- Tests updated to the correct contract: `delegate_tools_test.go` (Foreground + Background),
  `delegate_abort_test.go` (goroutine-driven; helpers `waitForDelegateChild[Stopped]`),
  `agent_tools_test.go`.

### D3 — compaction parity
- `internal/event/event.go`: added `TypeSessionNextCompaction{Started,Delta,Ended}` + props +
  constructors `NewSessionNextCompaction{Started,Delta,Ended}` (carry `timestamp,sessionID,
  messageID,reason,text,recent`). Legacy `compaction.*` / `session.compact*` kept for old clients.
- `internal/server/dcp_handlers.go`: `compactSession` now takes `reason`, generates a shared
  compaction `messageID`, emits started → delta(s during summary stream) → ended (balanced even
  when nothing compressible).
- `internal/server/v2_handlers.go`: removed the compact **double-publish**; `/api/session/{id}/context`
  now returns **active context messages** (after last compaction boundary) via `mapToV2Message`.
- `internal/server/agent_loop.go:~428`: auto-compaction tagged `reason:"auto"`.

### /compact fix
- `internal/server/session_handlers.go`: `handleSessionSummarize` now performs **real compaction**
  (calls `compactSession(reason:"manual")`) in addition to title refresh. THIS is what the TUI's
  `/compact` calls.
- `internal/server/tui_handlers.go`: `handleTUIExecuteCommand` no longer 501s — publishes
  `tui.command.execute` and returns `true` (upstream parity).
- `internal/server/tui_conformance_test.go`: removed stale "execute-command → 501" assertion.
- New tests: `internal/server/compact_command_test.go`
  (`TestTUIExecuteCommandPublishes`, `TestSummarizeTriggersCompaction`).
- New file `internal/server/dcp_handlers_test.go` adds `TestCompactEmitsCanonicalNextEvents`.

### D2 verification (no production code change)
- Added `TestDelegateChildEventOrdering` in `delegate_tools_test.go`: proves foreground
  delegation emits the child lifecycle in coherent order (created … busy … messages … idle),
  i.e. the async-race "wrong order" is gone. Confirmed existing `TestTUIPrompt_EventSequence`,
  `TestMonotonicGlobalSeq`, `TestAppendTextDeltaOrdering` still green.

**All changed files (uncommitted, not yet committed by request):** event.go, agent_loop.go,
dcp_handlers.go, delegate_tools.go, session_handlers.go, tui_handlers.go, v2_handlers.go,
+ their tests; new: compact_command_test.go, dcp_handlers_test.go.

---

## 5. Remaining work

> D1, D3, D4, /compact are DONE. D2 and D5 are ASSESSED (see §3) and need no blind changes —
> only revisit them WITH a real-TUI reproduction. What genuinely remains:

### Real-TUI verification (highest value — needs the user's environment)
Run the upstream TS TUI against the Go server (`README.md:37-62`, `OPENCODE_USE_GO_SERVER=1`) and
exercise: a prompt, a subagent delegation, `/compact`, an overflow auto-compaction. Confirm the four
felt symptoms are gone. This is the only way to (a) close D2/D5 honestly and (b) catch anything the
static contract missed. If a concrete ordering bug appears, THEN revisit D2 seq semantics.

### D5 deferred items (only if real-TUI shows a problem)
- `/sync/history` (`router.go:85`): hardcoded `{"data":[]}` → real events or typed non-2xx.
- `/experimental/worktree|workspace|move-session` (`router.go`, `handleTUIOKBool`): `200 true`
  no-ops → implement or typed unsupported. (`/formatter`,`/lsp` are already correct — leave them.)

### D2 deferred (only with a reproduction)
- Go per-part `GlobalSeq` (`internal/session/store.go:17,102-107`; `session.go:93`) vs upstream
  per-message `seq` (`/tmp/sst-opencode/packages/core/src/session/sql.ts`). Do NOT rewrite blind.

### D6 — DONE (assessed, no change needed; see §3)

### Optional: Stage 0 conformance harness
Only worth building if real-TUI verification surfaces protocol drift that unit tests miss.
Replay create→prompt→tool→delegate→compact→abort, capture SSE+HTTP, assert vs §6 contract.
- Todo `position` field (`internal/session/todo.go`); upstream `session/todo.ts` orders by `position`.
- Derive `nonCachedInputTokens = input - cached` (`internal/provider/openai.go:~318`) — cosmetic.

### Stage 0 — conformance harness (infra; build when D2 needs it)
Build a Go test harness that replays a scripted scenario (create→prompt→tool→delegate→compact→abort)
against the Go server, captures SSE+HTTP, and asserts vs the upstream contract in §6. Put under
`internal/server/conformance/`. Reuse `tui_conformance_test.go` SSE helpers (`sseEvents`, `waitEvent`).

---

## 6. Upstream 1.17.9 wire contract (extracted reference)

SSE envelope: `{id, type, properties}`; first event `server.connected` (`properties:{}`).
Message ordering: per-message `seq` integer, unique `(session_id, seq)` (`core/src/session/sql.ts`).

Key `session.next.*` events (all carry `timestamp,sessionID`; source `core/src/session/event.ts`):
- text: `text.started{assistantMessageID,textID}` / `text.delta{...,delta}`(ephemeral) / `text.ended{...,text}`
- tool: `tool.input.started{callID,name}` / `input.delta` / `input.ended` / `tool.called{callID,tool,input}` / `tool.success{...,result}` / `tool.failed{...,error}`
- step: `step.started{agent,model}` / `step.ended{finish,cost,tokens:{input,output,reasoning,cache:{read,write}}}` / `step.failed`
- compaction: `compaction.started{messageID,reason}` / `compaction.delta{messageID,text}`(ephemeral) / `compaction.ended{messageID,reason,text,recent}`  ← Go now matches
- reasoning.*, shell.*, retried, prompted/prompt.admitted. Plus legacy `session.idle`, `session.status{status:{type}}`.

HTTP (paths the TUI/SDK actually use — verify in `packages/sdk/js/src`):
- `POST /session` → `{data:{id,...}}`
- `POST /session/{id}/summarize` body `{providerID,modelID}` → **performs compaction** (NOT just title). THIS is `/compact`.
- `GET /session/{id}/context` → `{data:[messages after last compaction]}`
- `POST /api/session/{sessionID}/compact` (v2 alt path)
- `task` tool: foreground default; background gated by `OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS`.

TUI command mapping: `/compact` = `session.compact` command = `sdk.client.session.summarize(...)`
(`/tmp/sst-opencode/packages/tui/src/routes/session/index.tsx:~556`).

---

## 7. How to build & test (IMPORTANT caveat)

```sh
cd /home/thinh0704hcm/opencode-go
go build ./...
go test ./internal/event ./internal/session -count=1          # fast, always run these
```

**The full `./internal/server` suite has a PRE-EXISTING timeout (>200s)** — proven independent of
this session's changes via `git stash` comparison. Causes: a model test makes a **real network/http2
call** that hangs; several doom-loop integration tests take ~19s each; `Daytona`/`Devcontainer`
tests need real infra. **Do NOT treat this timeout as your regression.** Run **targeted** subsets:

```sh
# Touched-area regression net (clean, ~40s):
go test ./internal/server -run 'TestDCP|TestCompact|TestSummariz|TestTUIExecute|TestTUISummarize|TestUnsupportedStubs|TestDelegate|TestParentAbort|TestSessionContext|TestTUIV2_Stubs' -count=1 -timeout 150s

# Always skip infra/network tests in broad runs:
go test ./internal/server -skip 'Daytona|Devcontainer|DevContainer' -run '<your area>' -timeout 150s
```

To eyeball the real TUI end-to-end: see `README.md:37-62` (`OPENCODE_USE_GO_SERVER=1`, put upstream
TS binary on PATH as `opencode-ts`). Bot/SDK happy path: `OPENCODE_GO_MOCK=1 go run ./cmd/opencode-go serve`.

MCP boot needs `OPENCODE_GO_MCP=1` (off in tests). The `gitnexus`/`tg-bot-go` "MCP server starting"
lines in test output are noise from the dev session's own MCP servers, not the tests.

---

## 8. Open decisions for the user

- **D2 scope:** whether to re-base seq semantics to upstream per-message `seq` depends on a confirmed
  TUI ordering bug. Get a reproduction first.
- **Commit cadence:** changes are uncommitted. User has not asked to commit/push yet. On commit, branch
  off `main` first; co-author trailer per repo convention.
- **`server.pid`** is tracked and dirty in git — likely should be gitignored (ask before changing).
