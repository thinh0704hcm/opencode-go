# opencode-go (Milestone M1)

A minimal Go reimplementation of the opencode HTTP/SSE server, scoped to the
**tg-bot-go bot happy path** (Milestone M1). It implements just enough of the
opencode 1.16.0 wire contract for the Telegram bot to create a session, stream a
prompt, and recover the transcript — using an OpenAI-compatible provider or a
tokenless mock.

This is a separate Go module (`github.com/opencode-go/opencode-go`); it does not
modify or depend on `tg-bot-go` or any opencode/SDK package.

Architecture reference: `tg-bot-go/docs/opencode-go-architecture.md`.

## Build & run

```sh
go build ./...
go test ./...                      # all tests pass with the mock; NO network/tokens

# Run with the mock provider (no API key needed):
OPENCODE_GO_MOCK=1 go run ./cmd/opencode-go serve --hostname 127.0.0.1 --port 4096

# Run against a real OpenAI-compatible provider:
OPENCODE_GO_BASE_URL=https://api.example.com/v1 \
OPENCODE_GO_API_KEY=sk-... \
OPENCODE_GO_MODEL=cx/gpt-5.5 \
  go run ./cmd/opencode-go serve --port 4096
```

### `serve` flags

| Flag | Default | Notes |
|---|---|---|
| `--hostname` | `127.0.0.1` | Loopback only. Binding a non-loopback host is refused. |
| `--port` | `4096` | TCP port. |

## Environment variables

| Var | Purpose |
|---|---|
| `OPENCODE_GO_MOCK` | When `=1`, use the MOCK provider that streams a fixed short reply token-by-token. Takes precedence over the real provider, so M1 is testable without burning API tokens. |
| `OPENCODE_GO_BASE_URL` | Base URL of the OpenAI-compatible provider. The client POSTs `{baseURL}/chat/completions`. |
| `OPENCODE_GO_API_KEY` | Bearer key sent as `Authorization: Bearer <key>`. |
| `OPENCODE_GO_MODEL` | Model id. Accepts `providerID/modelID` (the part after `/` is sent on the wire) or a bare model string. |

If neither `OPENCODE_GO_MOCK=1` nor both `OPENCODE_GO_BASE_URL` + `OPENCODE_GO_API_KEY`
are set, the server falls back to the MOCK provider and logs a warning.

## M1 endpoint set

All endpoints accept an optional `?directory=<cwd>` query param. In M1 it is
threaded through (and echoed into the `/global/event` envelope) but otherwise
unused.

| Method | Path | Behavior |
|---|---|---|
| `GET` | `/global/health` | `200` JSON `{"healthy":true,"version":"1.16.0"}`. |
| `GET` | `/api/global/health` | Same JSON as above (bot probes this; only checks 2xx). |
| `GET` | `/global/event` | `text/event-stream`. Immediately sends `server.connected`, then streams events wrapped as `{"directory":<cwd>,"payload":<Event>}`. Heartbeat comment every ~15s. |
| `GET` | `/event` | Same stream, bare `Event` payload (no envelope). |
| `POST` | `/session` | Accepts `{}` (and optional `{parentID,title}`); returns `200` Session `{id:"ses_...",title,time:{created,updated},directory}`. |
| `POST` | `/session/{id}/prompt_async` | Body `{messageID?,model:{providerID,modelID},agent,parts:[{type:"text",text}]}`. Returns `204` immediately; runs generation in a background goroutine. Unknown session -> `404`. |
| `GET` | `/session/{id}/message` | JSON array of `{info:{id,role,sessionID,time:{created,completed}}, parts:[{id,messageID,type,text}]}`. Unknown session -> `404`. |
| `POST` | `/permission/{requestID}/reply` | Body `{"reply":"once\|always\|reject"}` (bot primary). Unknown id -> `404`. |
| `POST` | `/session/{sessionID}/permissions/{permissionID}` | Body `{"response":"once\|always\|reject"}` (fallback). Wired to the same gate. Unknown id -> `404`. |

### Event terminal contract (locked Option A)

Real opencode 1.16.0 never emits `session.idle`. opencode-go, per generation,
emits in order:

```
message.updated (user)
session.status {type:"busy"}
message.updated (assistant, time.completed = null)
message.part.delta (field:"text") AND message.part.updated (full text snapshot)   [repeated]
message.updated (assistant, time.completed set)        [GUARANTEED-DELIVERY]
session.idle {sessionID}                                [GUARANTEED-DELIVERY, synthetic]
```

All `properties` use capital-ID keys (`sessionID`, `messageID`, `partID`). The
M1 agent loop is a single assistant turn — there is no real tool loop yet.

### Event bus backpressure (split policy)

- **Droppable:** `message.part.delta` — dropped with a non-blocking send when a
  subscriber buffer is full (recoverable via `message.part.updated` snapshots and
  `GET .../message`).
- **Guaranteed-delivery:** `session.idle`, `session.error`, `permission.asked`,
  `permission.updated`, and the final assistant `message.updated` — block with a
  bounded timeout; a wedged subscriber is evicted (its stream closed) rather than
  allowed to stall the prompt worker.

## Security posture (no auth — accepted risk)

- The server binds **`127.0.0.1` only**. Binding a routable interface is refused.
- There is **no authentication** on the HTTP surface, matching opencode's
  localhost posture. Any local process that can reach the loopback port can drive
  the server (create sessions, spend provider tokens). This is an explicitly
  accepted risk for a single-operator loopback deployment. If the host trust
  boundary changes, add an auth check before exposing beyond loopback.

## Package layout

```
cmd/opencode-go/main.go        # CLI: `serve` subcommand, provider wiring from env
internal/server/               # router, handlers, SSE, generation worker
internal/event/                # Event type + constructors + bus + SSE framing
internal/session/              # in-memory store (RWMutex, deep-copy reads) + types
internal/provider/             # OpenAI-compatible streaming client + mock
internal/permission/           # permission store wired to one reply gate
```

## Not in M1

On-disk persistence, the full TUI boot endpoint set, tools/sandbox, the real
agent/tool loop, PTY, MCP, LSP, VCS, and provider/model discovery are out of
scope for M1 (see the architecture doc milestones M2–M5).
