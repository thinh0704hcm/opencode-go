# Plan: `@opencode-ai/sdk` Drop-In Replacement

**Goal:** Make `opencode-go` a complete drop-in replacement for the `opencode` server binary so
that `createOpencodeServer()` / `createOpencodeClient()` from `@opencode-ai/sdk@1.17.4` work
without modification. The TUI remains the original TypeScript binary; only the HTTP server that
it talks to is replaced by this Go implementation.

---

## Background

The SDK has two responsibilities:

1. **Process management** — `createOpencodeServer()` spawns `opencode serve --hostname=... --port=...`
   and waits for a specific line on stdout before resolving the URL.
2. **HTTP client** — `createOpencodeClient()` constructs a typed client that talks to the running
   server over the v1 API (un-prefixed routes) and the v2 API (`/api/` prefix).

The SDK also passes `OPENCODE_CONFIG_CONTENT=<json>` in the subprocess environment and optionally
`--log-level=<level>` as a CLI flag.

---

## Gap Inventory

### Gap 1 — Startup stdout message (CRITICAL)

**File:** `cmd/opencode-go/main.go`

The SDK parses stdout for two patterns (both v1 `dist/server.js` and v2 `dist/v2/server.js`):

```
line.startsWith("opencode server listening")
line.match(/on\s+(https?:\/\/[^\s]+)/)
```

Expected exact format:
```
opencode server listening on http://127.0.0.1:4096
```

Current behavior: `main.go` logs via `slog.NewTextHandler(os.Stderr, ...)` which emits:
```
time=2026-06-14T00:22:00Z level=INFO msg="opencode-go listening" addr=127.0.0.1:4096 auth=...
```
This goes to **stderr**, not stdout, and does not start with `"opencode server listening"`.
`createOpencodeServer()` hits its 5-second timeout immediately and rejects with:
> `Timeout waiting for server to start after 5000ms`

**Fix:** After `net.Listen` succeeds (or immediately before `ListenAndServe`), emit exactly:
```go
fmt.Fprintf(os.Stdout, "opencode server listening on http://%s\n", addr)
```
to stdout (unbuffered). Keep all other logging on stderr via slog.

---

### Gap 2 — `OPENCODE_CONFIG_CONTENT` environment variable

**File:** `internal/config/config.go` → `Load()`

The SDK sets:
```js
env: { ...process.env, OPENCODE_CONFIG_CONTENT: JSON.stringify(options.config ?? {}) }
```

`config.Load()` currently reads files in this order (lowest → highest precedence):
1. `$OPENCODE_CONFIG` path
2. `./opencode.jsonc`
3. `./.opencode/opencode.jsonc`
4. `~/.config/opencode/opencode.json(c)`
5. `<directory>/.opencode/opencode.json(c)` (project overlay)

`OPENCODE_CONFIG_CONTENT` is **not** in this chain, so any config passed by the SDK (e.g. a
custom `logLevel`, `model`, or provider block) is silently dropped.

**Fix:** At the end of `Load()`, after the project overlay is applied but before env-var
interpolation, check `os.Getenv("OPENCODE_CONFIG_CONTENT")`. If non-empty, parse it as JSON and
deep-merge it over the accumulated map (highest precedence). Then proceed with snapshotting
`rawNoEnv` and calling `interpolateEnv`.

```go
// highest-precedence overlay from SDK / CI callers
if raw := os.Getenv("OPENCODE_CONFIG_CONTENT"); raw != "" {
    var overlay map[string]any
    if json.Unmarshal([]byte(raw), &overlay) == nil {
        merged = mergeMaps(merged, overlay)
    }
}
```

---

### Gap 3 — `--log-level` flag

**File:** `cmd/opencode-go/main.go` → `runServe()`

The SDK passes `--log-level=<level>` when `options.config.logLevel` is set:
```js
if (options.config?.logLevel) args.push(`--log-level=${options.config.logLevel}`)
```

`runServe` uses `flag.FlagSet` with `ContinueOnError`. An unknown flag causes `Parse` to return
an error, which bubbles up and kills the process before the server ever starts.

**Fix:** Declare the flag (wire it to the slog level):
```go
logLevel := fs.String("log-level", "info", "log level (debug|info|warn|error)")
```
Then map the string to `slog.Level` before constructing the logger. Accepted values:
`debug`, `info`, `warn`/`warning`, `error` (case-insensitive). Unknown values → `info`.

---

### Gap 4 — Missing v1 API routes

These are called by the v1 SDK client (`dist/gen/sdk.gen.js`) and are **not registered** in
`internal/server/router.go`.

| Method | Path | SDK caller | Suggested handler |
|--------|------|------------|-------------------|
| `PATCH` | `/config` | `Config.update()` | `handleConfigUpdate` — parse JSON body, return `{}` (noop); real impl can persist later |
| `POST` | `/mcp` | `Mcp.add()` | `handleMCPAdd` — noop, return `{}` |
| `PUT` | `/auth/{id}` | `Auth.set()` (v1 MCP auth.set) | `handleAuthSet` — noop, return `{}` |
| `DELETE` | `/mcp/{name}/auth` | `Auth.remove()` | `handleMCPAuthRemove` — noop, return `{}` |

**Router additions** (in `router.go`):
```go
mux.HandleFunc("PATCH /config",             s.handleConfigUpdate)
mux.HandleFunc("POST /mcp",                 s.handleMCPAdd)
mux.HandleFunc("PUT /auth/{id}",            s.handleAuthSet)
mux.HandleFunc("DELETE /mcp/{name}/auth",   s.handleMCPAuthRemove)
```

---

### Gap 5 — Missing v2 global routes

Called by the v2 SDK (`dist/v2/gen/sdk.gen.js`). None of these have `/api/` prefix — they use
the same un-prefixed paths as v1. The TUI accesses them during boot and background polling.

#### 5a — `/global/config`
| Method | Path | SDK caller |
|--------|------|------------|
| `GET` | `/global/config` | `Global.config.get()` |
| `PATCH` | `/global/config` | `Global.config.update()` |

`GET` returns the same shape as `/config` (reuse `handleConfigGet` logic).
`PATCH` is a noop returning `{}`.

**Router:**
```go
mux.HandleFunc("GET /global/config",   s.handleGlobalConfigGet)
mux.HandleFunc("PATCH /global/config", s.handleGlobalConfigUpdate)
```

#### 5b — `/global/dispose` and `/global/upgrade`
| Method | Path | SDK caller |
|--------|------|------------|
| `POST` | `/global/dispose` | `Global.dispose()` |
| `POST` | `/global/upgrade` | `Global.upgrade()` |

Both are noops returning `{}`. (Upgrade has no meaning for a Go binary that's not self-updating.)

**Router:**
```go
mux.HandleFunc("POST /global/dispose", s.handleTUIOK)
mux.HandleFunc("POST /global/upgrade", s.handleTUIOK)
```

#### 5c — Provider auth (v2)
| Method | Path | SDK caller |
|--------|------|------------|
| `DELETE` | `/auth/{providerID}` | `Auth.remove()` |
| `PUT` | `/auth/{providerID}` | `Auth.set()` |

Both are noops returning `{}` (auth persistence is out of scope for M1).

**Router:**
```go
mux.HandleFunc("DELETE /auth/{providerID}", s.handleAuthRemove)
mux.HandleFunc("PUT /auth/{providerID}",    s.handleAuthSet)
```

Note: v1 uses `PUT /auth/{id}` (from Gap 4) and v2 uses `PUT /auth/{providerID}` — both path
patterns are equivalent; a single handler can serve both if registered under both patterns, or
registered once with the `{id}` wildcard which matches either segment name.

---

### Gap 6 — Missing experimental stubs

The TUI calls these during boot/polling. All are safe to stub as empty collections or `{}`.

#### 6a — Console org management
| Method | Path | SDK caller | Response |
|--------|------|------------|---------|
| `GET` | `/experimental/console/orgs` | `Console.listOrgs()` | `[]` |
| `POST` | `/experimental/console/switch` | `Console.switchOrg()` | `{}` |

**Router:**
```go
mux.HandleFunc("GET /experimental/console/orgs",    s.handleExperimentalConsoleOrgs)
mux.HandleFunc("POST /experimental/console/switch", s.handleTUIOK)
```

#### 6b — Experimental session list
| Method | Path | SDK caller | Response |
|--------|------|------------|---------|
| `GET` | `/experimental/session` | `Experimental.session.list()` | `{"items":[],"cursor":null}` |

**Router:**
```go
mux.HandleFunc("GET /experimental/session", s.handleExperimentalSessionList)
```

Return shape (matches v2 paginated response):
```json
{ "items": [], "cursor": null }
```

#### 6c — Control plane
| Method | Path | SDK caller | Response |
|--------|------|------------|---------|
| `POST` | `/experimental/control-plane/move-session` | `ControlPlane.moveSession()` | `{}` |

**Router:**
```go
mux.HandleFunc("POST /experimental/control-plane/move-session", s.handleTUIOK)
```

#### 6d — Workspace management (7 endpoints)
| Method | Path | Response |
|--------|------|---------|
| `GET` | `/experimental/workspace/adapter` | `[]` |
| `POST` | `/experimental/workspace` | `{}` |
| `POST` | `/experimental/workspace/sync-list` | `{}` |
| `DELETE` | `/experimental/workspace/{id}` | `{}` |
| `POST` | `/experimental/workspace/warp` | `{}` |

Note: `GET /experimental/workspace` and `GET /experimental/workspace/status` **already exist**
in the router.

**Router additions:**
```go
mux.HandleFunc("GET /experimental/workspace/adapter",    s.handleExperimentalWorkspaceAdapter)
mux.HandleFunc("POST /experimental/workspace",           s.handleTUIOK)
mux.HandleFunc("POST /experimental/workspace/sync-list", s.handleTUIOK)
mux.HandleFunc("DELETE /experimental/workspace/{id}",    s.handleTUIOK)
mux.HandleFunc("POST /experimental/workspace/warp",      s.handleTUIOK)
```

#### 6e — Worktree management (4 endpoints)
| Method | Path | Response |
|--------|------|---------|
| `GET` | `/experimental/worktree` | `[]` |
| `POST` | `/experimental/worktree` | `{}` |
| `DELETE` | `/experimental/worktree` | `{}` |
| `POST` | `/experimental/worktree/reset` | `{}` |

**Router additions:**
```go
mux.HandleFunc("GET /experimental/worktree",          s.handleExperimentalWorktreeList)
mux.HandleFunc("POST /experimental/worktree",         s.handleTUIOK)
mux.HandleFunc("DELETE /experimental/worktree",       s.handleTUIOK)
mux.HandleFunc("POST /experimental/worktree/reset",   s.handleTUIOK)
```

#### 6f — Project copy management (3 endpoints)
| Method | Path | Response |
|--------|------|---------|
| `POST` | `/experimental/project/{projectID}/copy` | `{}` |
| `DELETE` | `/experimental/project/{projectID}/copy` | `{}` |
| `POST` | `/experimental/project/{projectID}/copy/refresh` | `{}` |

**Router additions:**
```go
mux.HandleFunc("POST /experimental/project/{projectID}/copy",         s.handleTUIOK)
mux.HandleFunc("DELETE /experimental/project/{projectID}/copy",       s.handleTUIOK)
mux.HandleFunc("POST /experimental/project/{projectID}/copy/refresh", s.handleTUIOK)
```

---

### Gap 7 — Binary name

The SDK hardcodes the binary name `opencode`:
```js
const proc = launch(`opencode`, args, { ... })
```

The Makefile builds to `bin/opencode-go`. For the SDK to find the Go binary it must be
installed somewhere in `$PATH` as `opencode`, or the user must symlink it.

**Fix:** Add a `bin/opencode` build target to the Makefile:
```makefile
build-sdk: ## Build binary as bin/opencode (SDK-compatible name)
    go build -o bin/opencode ./cmd/opencode-go
```

And document in `README.md` that `sudo ln -sf $(pwd)/bin/opencode /usr/local/bin/opencode`
(or equivalent) is required for the SDK's process-launch path. The HTTP client path works
with any name as long as the URL is provided directly.

---

## Implementation Order

Execute in this order to keep the server runnable at every step.

### Phase 1 — Boot contract (unblocks all SDK usage)

These three changes must land together; without them `createOpencodeServer()` always fails.

1. **`cmd/opencode-go/main.go`**
   - Add `--log-level` flag declaration in `runServe`
   - Parse it into a `slog.Level` and pass to `slog.NewTextHandler`
   - After `net.Listen` (or just before `ListenAndServe`), print:
     `fmt.Fprintf(os.Stdout, "opencode server listening on http://%s\n", addr)`

2. **`internal/config/config.go`**
   - In `Load()`, after the project overlay `mergeMaps` call, insert the
     `OPENCODE_CONFIG_CONTENT` overlay as the highest-precedence layer.

### Phase 2 — Missing v1 routes

These are required for full v1 SDK client parity.

3. **`internal/server/router.go`** — add 4 route registrations (from Gap 4)
4. **`internal/server/mcp_handlers.go`** — add `handleMCPAdd`, `handleMCPAuthRemove`
5. **`internal/server/config_handlers.go`** — add `handleConfigUpdate`
6. **`internal/server/handlers.go`** (or new `auth_handlers.go`) — add `handleAuthSet`

### Phase 3 — Missing v2/experimental routes

These are required for the newer TUI clients and full v2 SDK parity.

7. **`internal/server/router.go`** — add all route registrations from Gaps 5 and 6
8. **`internal/server/boot_handlers.go`** — add:
   - `handleGlobalConfigGet` (delegates to config load, same shape as `/config`)
   - `handleGlobalConfigUpdate` (noop `{}`)
   - `handleExperimentalConsoleOrgs` (returns `[]`)
   - `handleExperimentalSessionList` (returns `{"items":[],"cursor":null}`)
   - `handleExperimentalWorkspaceAdapter` (returns `[]`)
   - `handleExperimentalWorktreeList` (returns `[]`)

All other new endpoints reuse the existing `handleTUIOK` (returns `200 {}`).

### Phase 4 — Build tooling

9. **`Makefile`** — add `build-sdk` target producing `bin/opencode`
10. **`README.md`** — add "SDK integration" section documenting the PATH install step

---

## File Change Summary

| File | Change type | Phase |
|------|-------------|-------|
| `cmd/opencode-go/main.go` | Modify — stdout line + `--log-level` flag | 1 |
| `internal/config/config.go` | Modify — `OPENCODE_CONFIG_CONTENT` overlay | 1 |
| `internal/server/router.go` | Modify — ~20 new route registrations | 2+3 |
| `internal/server/config_handlers.go` | Modify — `handleConfigUpdate` | 2 |
| `internal/server/mcp_handlers.go` | Modify — `handleMCPAdd`, `handleMCPAuthRemove` | 2 |
| `internal/server/boot_handlers.go` | Modify — 6 new stub handlers | 3 |
| `internal/server/handlers.go` | Modify (or new file) — `handleAuthSet`, `handleAuthRemove` | 2+3 |
| `Makefile` | Modify — `build-sdk` target | 4 |

---

## Acceptance Criteria

After all phases:

```js
import { createOpencodeServer, createOpencodeClient } from "@opencode-ai/sdk"

const server = await createOpencodeServer({ timeout: 10000 })
// ✓ resolves without timeout (Gap 1 fixed)

const client = createOpencodeClient({ baseUrl: server.url })

await client.session.list()    // ✓ returns []
await client.config.get()      // ✓ returns config object
await client.config.update()   // ✓ returns {} (Gap 4)
await client.mcp.add()         // ✓ returns {} (Gap 4)
await client.mcp.auth.set()    // ✓ returns {} (Gap 4)

server.close()
```

And with v2:
```js
import { createOpencodeServer, createOpencodeClient } from "@opencode-ai/sdk/v2"

const server = await createOpencodeServer({ timeout: 10000 })
const client = createOpencodeClient({ baseUrl: server.url })

await client.global.config.get()                    // ✓ (Gap 5a)
await client.global.dispose()                       // ✓ (Gap 5b)
await client.experimental.session.list()            // ✓ (Gap 6b)
await client.experimental.workspace.list()          // ✓ already existed
await client.experimental.workspace.adapter.list()  // ✓ (Gap 6d)

server.close()
```

---

## Notes

- All "noop" handlers must return `Content-Type: application/json` and a valid JSON body.
  The v2 client throws if `Content-Type` is `text/html` (see `dist/v2/client.js` interceptor).
- The `handleTUIOK` helper already satisfies this — it writes `200 {}` with JSON content type.
- `GET /experimental/session` is **not** the same as `GET /api/session` (v2 path). Both must
  exist independently.
- The `--log-level` flag does not need to affect the startup stdout line; that line is always
  emitted regardless of log level.
- `OPENCODE_CONFIG_CONTENT` JSON is merged with `mergeMaps` (shallow-recursive same as file
  config), so nested provider blocks work correctly.
