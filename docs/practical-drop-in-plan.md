# Practical hybrid drop-in plan

## Goal

Use `opencode-go` for server/SDK paths while preserving the original TypeScript CLI/TUI.

## Blockers

The Go server is currently an experimental fallback. It lacks context/session parity, subtask support, and tool parity compared to the original TypeScript server. For safety, the TypeScript server is default for all operations.

## Implemented path

1. Build Go server as `bin/opencode-go`.
2. Install wrapper as `bin/opencode`.
3. Keep original TS binary available as `opencode-ts`.
4. Wrapper dispatch:
   - `OPENCODE_USE_GO_SERVER=1 opencode serve ...` → `opencode-go serve ...`
   - `OPENCODE_USE_GO_SERVER=1 opencode models ...` → `opencode-go models ...`
   - everything else (or without `OPENCODE_USE_GO_SERVER=1`) → `opencode-ts ...`

## Backlog

- Package helper that installs/renames the upstream TS CLI to `opencode-ts`.
- Broaden SDK smoke coverage beyond health/session/config.
- Add CI job with extracted `@opencode-ai/sdk` fixture and `cross-spawn` installed.
- Resolve parity blockers before enabling Go server by default.
