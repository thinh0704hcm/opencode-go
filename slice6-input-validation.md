# Slice 6: Input Validation + Metadata Interrupt Parity

## Goal
Fix 10 API test failures + 1 TS parity gap (metadata.interrupted). All scoped parity/sad-path fixes, no architectural changes.

## Tasks

- [ ] **6A: metadata.interrupted on abort** — In `agent_loop.go:367`, after `AppendToolPart` returns part `p`, set `p.State.Metadata["interrupted"] = true` before publishing events. Verify: `go test ./internal/server/... -run Abort`
- [ ] **6B: POST /message content validation** — In `handlers.go` `handlePrompt`, after extracting texts, return 400 if no non-empty text part found. Add `io.LimitReader` (1MB) to request body in `decodeBody` or before it. Verify: `curl -X POST .../message -d '{"content":""}'` → 400; `curl -X POST .../message -d '{"content":123}'` → 400
- [ ] **6C: POST /vcs/apply → 400 on empty body** — In `vcs_handlers.go:171`, change `http.StatusOK` to `http.StatusBadRequest` for empty body. Verify: `curl -X POST .../vcs/apply -d '{}'` → 400
- [ ] **6D: PATCH /config validate JSON** — In `config_handlers.go` `handleConfigUpdate`, read body, attempt JSON decode, return 400 on failure instead of `handleTUIOK`. Verify: `curl -X PATCH .../config -d 'garbage'` → 400
- [ ] **6E: GET /skill stub** — In `router.go`, add `mux.HandleFunc("GET /skill", ...)` returning 200 with `[]`. Verify: `curl .../skill` → 200
- [ ] **6F: Update api test scripts** — Fix test expectations to match corrected HTTP status codes. Re-run both scripts.
- [ ] **6G: Build + test** — `go build ./...` && `go test ./internal/server/...` && `go test ./internal/session/...`
- [ ] **6H: reviewer-escalation proofread** — Before closing, reviewer-escalation on all changed files.

## Done When
- All 10 API test failures resolved
- metadata.interrupted parity confirmed
- `go build ./...` pass, all tests pass
