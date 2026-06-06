# opencode-go — Status

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
