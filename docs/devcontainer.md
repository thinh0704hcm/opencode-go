# Devcontainer Feature

**Purpose**: Experimental, environment‑gated Docker bootstrap that runs a command in an isolated container bound to the session workdir.

## Environment Variables
- `DEV_CONTAINER_ENABLED` – enable feature when set to a truthy value (`1`, `true`, `yes`).
- `DEV_CONTAINER_IMAGE` – **required** Docker image to run (e.g. `alpine:latest`).
- `DEV_CONTAINER_TIMEOUT` – timeout in seconds, default **300**.

## HTTP Endpoint
```
POST /experimental/devcontainer/bootstrap
```
### Request JSON
```json
{ "sessionID": "<session-id>", "cmd": ["<executable>", "<arg>..."] }
```
### Response JSON
```json
{ "output": "<stdout+stderr>", "error": "<error message>" }
```
- `200 OK` – command succeeded, `error` omitted.
- `400 Bad Request` – malformed body, missing `sessionID`, or `DEV_CONTAINER_IMAGE` not configured.
- `403 Forbidden` – devcontainer disabled.
- `500 Internal Server Error` – Docker runner error.

## Security Notes
- Runs with `--network none` (no network access).
- Runs as the invoking UID:GID (`--user <uid:gid>`).
- Container is **read‑only** (`--read-only`).
- All Linux capabilities dropped (`--cap-drop ALL`).
- `--security-opt no-new-privileges` prevents privilege escalation.
- Process count limited to **512** (`--pids-limit 512`).
- Workdir is bind‑mounted **read‑write** at `/work`.

These flags provide defense‑in‑depth while still allowing the command to write to the session workdir.
