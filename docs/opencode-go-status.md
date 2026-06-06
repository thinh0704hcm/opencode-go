# opencode-go — Status

## Review status (final)
- Verdict: **SHIP** for single-user loopback deployment (independent reviewer pass + re-review confirmed).
- Memory: opencode-go idle RSS ~8.8 MB vs ~243 MB headless Node opencode (~27x lighter), ~630-660 MB full Node TUI session (~70x lighter).
- All CRITICAL + MAJOR review findings fixed and verified in live code:
  - C1 PTY child reaping (no zombies)
  - C2 bash output capped during capture (OOM vector closed)
  - M1 loopback-bind enforcement + mandatory PTY connect ticket (no unauth shell)
  - M2 closeSubs sets closed flag (no post-EOF subscriber hang)
  - M3 bash surfaces non-zero exit status
  - M4 non-blocking guaranteed event delivery (no per-subscriber 5s stall)
- Minors fixed: aborted-turn idle dedup, provider-error secret scrubbing, wider secret regex, errors.Is(io.EOF), dead-code removal (maskConfigAPIKeys, itoa), PTY ticket-map expiry sweep.
- Residual (non-blocking, documented): sandbox O_NOFOLLOW helper added but tool I/O not yet rewired to use it (low-risk TOCTOU on single-tenant loopback); token cost shows 0 (no pricing table); /vcs/apply, live MCP client, some experimental/* are stubs.
- Quality gates: go build / go vet / go test -race ./... all green.

## Goal / Scope
opencode-go is a from-scratch Go reimplementation of the opencode server (targets
opencode 1.16.0 wire protocol), built as a memory-light, runtime-free (no Bun/Node)
DROP-IN REPLACEMENT serving BOTH:
- the tg-bot-go Telegram bot (uses opencode purely as transport)
- the opencode terminal TUI (`opencode attach http://127.0.0.1:<port>`)

Driver: reduce memory footprint + remove the Bun/Node runtime on a resource-limited
host. Scope decision = "Option A" (full drop-in incl. TUI).

## Milestones (all complete)
- **M1**: bot happy path (health, /global/event SSE, session create, prompt_async,
  messages, permissions)
- **M2**: 20-endpoint TUI boot set + `?directory=` middleware + secret redaction
- **M3**: session CRUD + synchronous prompt (POST /session/{id}/message)
- **M4**: tool registry + sandbox (traversal/symlink-escape blocked) + bounded
  permission-gated agent loop
- **M5**: VCS (real git), MCP (config-backed), PTY (creack/pty + websocket), LSP (stub)
- **Agent brain**: embedded opencode default.txt system prompt + real tool descriptions
- **Integrated shell**: POST /session/{id}/shell

## Verified working
- tg-bot-go bot-contract replay: **9/9** (health, SSE handshake, session create,
  prompt_async 204, deltas, session.idle, message recovery, both permission reply paths)
- TUI: chat renders correctly; agent executes terse; tools + permission gating;
  integrated shell runs commands and renders output (running->completed, single part)
- Real-provider smoke test (concactao gateway): **pass**
- `go build` / `go vet` / `go test -race ./...` : all green

## Key fixes discovered by diffing against real opencode (lessons)
- **Message/session IDs** must use opencode's exact format (6-byte big-endian
  `(ms<<12|counter)` hex + base62; ascending for msg/prt, descending for ses) — the
  TUI sorts by ID.
- **Assistant message** must include `tokens`, `cost` (always serialized, NOT
  `omitempty`), `parentID`, `model` fields — TUI reads them numerically; undefined
  silently fails render.
- **permission.updated** event (with Permission object incl. `always[]` array) is what
  the TUI listens for (not just `permission.asked`).
- **assistant tool_calls message must precede the tool-result message** (OpenAI
  protocol) or the model over-steps.
- **tool-part state** must be rich `{status, input, output, title, metadata, time}`;
  `AppendToolPart` upserts by callID (one part running->completed, not two).
- **PTY ws protocol**: text frames = raw output, one binary `0x00`+JSON cursor meta
  frame; single-owner buffered reader.
- **integrated shell** uses POST /session/{id}/shell, NOT /pty/* (request logging
  proved it).

## Known gaps / cleanup (non-blocking)
- `GET /session` returns 405 (only POST registered); `/session/{id}/todo` and `/diff`
  return 404 (unimplemented; TUI tolerates).
- Token/cost accounting shows 0 (provider usage parsing not wired) — cosmetic.
- `/vcs/apply`, MCP live client, some `experimental/*` are stubs.
- Real-TTY PTY `/pty/*` websocket path implemented + byte-verified but the current TUI
  build uses /session/{id}/shell instead.

## How to run
- **Dev (mock, tokenless)**:
  `OPENCODE_GO_MOCK=1 ./bin/opencode-go serve --hostname 127.0.0.1 --port 4182`
  (or `make run`)
- **Real provider**: set `OPENCODE_GO_BASE_URL`, `OPENCODE_GO_API_KEY`,
  `OPENCODE_GO_MODEL` then `make run-real`
- **Attach TUI**: `opencode attach http://127.0.0.1:4182`
- **Bot**: point tg-bot-go's `TG_BOT_OPENCODE_BIN` at the opencode-go binary
- **make targets**: `build`, `run`, `run-real`, `test-race`, `check`, `tui`, `kill`

## Commit history
The milestone commits are in `git log` (M1..M5 + agent brain + shell + render fixes).

## Commit log (key)
- M1 milestone: bot happy path (health, SSE, session create, prompt_async, messages, permissions)
- M2 milestone: 20-endpoint TUI boot set + ?directory= middleware + secret redaction
- M3 milestone: session CRUD + synchronous prompt (POST /session/{id}/message)
- M4 milestone: tool registry + sandbox + bounded permission-gated agent loop
- M5 milestone: VCS (real git), MCP (config-backed), PTY (creack/pty + websocket), LSP stub
- agent brain: embedded opencode default.txt system prompt + real tool descriptions
- integrated shell: POST /session/{id}/shell
- render-fidelity fixes: sortable IDs, cost/tokens always serialized, permission.updated,
  tool_calls threading, rich tool state, upsert-by-callID
- PTY ws protocol: text=raw output + binary 0x00+JSON cursor meta, buffered single-owner fan-out
- route cleanup: GET /session, /session/{id}/todo, /diff
- token-usage parsing
- review fixes:
  - 467ad30 security M1 (loopback bind + PTY connect ticket)
  - 4b60b96 C1/C2/M2/M3 (PTY reaping, bash output cap, closeSubs flag, non-zero exit)
  - b6030dd review-A
  - d3fd43d review-A2
  - 3289226 review-C cleanups
