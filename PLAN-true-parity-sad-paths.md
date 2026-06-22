# PLAN: True Parity & Sad Path Resolution

## Overview
The `opencode-go` port was initially built as a "happy path" implementation. It lacks robust handling for sad paths (disconnects, tool failures, infinite loops) and fakes advanced features to prevent the TUI from crashing. This plan outlines the exact phases and tasks required to achieve **True Parity** with the TypeScript base, ensuring robust error handling and genuine feature implementation.

## Phase 1: Critical Sad Paths & Runaways (Immediate Action)
These are the most destructive bugs currently burning tokens and breaking user experience.

### 1. Agent Infinite Loops (The Runaway Subagent)
* **Problem**: `runAgentLoop` is an unbounded `for` loop. If an agent hallucinates or repeatedly calls a forbidden tool (like `delegate` from within a subagent), it loops infinitely.
* **Solution**: 
  - Parse `steps` (fallback to `maxSteps`) from the agent config (`agents.go`).
  - Enforce a strict iteration limit in `runAgentLoop` (default 50).
  - Return a forced text response or explicit "error: exceeded maximum number of turns" when the limit is hit.

### 2. Disconnect & Interrupt Handling
* **Problem**: Closing the TUI drops the SSE connection, but leaves the background generation running forever because `r.Context().Done()` does not trigger a session abort.
* **Solution**:
  - Update `handleV2SessionEvent` to detect `r.Context().Done()`.
  - Check if the session is flagged as a "background" task. If not, automatically invoke `s.handleSessionAbort(sessionID)` to drain the queue and kill the generation.

### 3. Task Notifications & Context Inheritance
* **Problem**: Subagents lose parent context and fail to return results properly, causing them to hallucinate without direction.
* **Solution**:
  - Complete the pending items in `notification-fix.md`.
  - Ensure `getParentContext` accurately injects the parent's prompt into the child's context.
  - Fix `renderTaskResult` injection so the parent actually resumes when a child finishes or crashes.

---

## Phase 2: State & UI Parity (Data Accuracy)

### 4. Message Sequencing & TOCTOU
* **Problem**: Rapid events (tool calls + user interrupts) can race, causing `admittedSeq` monotonicity errors and out-of-order messages in the TUI.
* **Solution**:
  - Refactor `internal/server/store` to use a strict append-only event log (Redux-style dispatch) instead of bare `sync.Mutex` maps.
  - Ensure `admittedSeq` is strictly incremented and validated before broadcasting updates.

### 5. Context Statistics & Cost Tracking
* **Problem**: `Cost.Cache.Write` is improperly typed (`int64` instead of `float64`), and token estimation is wildly inaccurate (naive `len/4`).
* **Solution**:
  - Fix the type definitions in the Store to match TS exactly.
  - Implement a more accurate fallback token estimator or enforce strict usage reporting from the provider stream.

### 6. DCP Compression Notifications
* **Problem**: The TUI expects specific DCP metadata shapes when context is compressed, but `opencode-go` fails to provide them or formats them incorrectly.
* **Solution**:
  - Align `event.NewSessionDCPNotification` payloads with the TS schema.
  - Broadcast DCP events precisely when the prompt truncator fires.

### 7. Todo Tool Wiring
* **Problem**: `todo_tool.go` exists, but the TUI's GET endpoints for todos are returning 404 or empty arrays.
* **Solution**:
  - Implement the `GET /session/{id}/todo` route.
  - Wire the session store to persist and retrieve the todo list state natively.

---

## Phase 3: The "Hoaxes" (True Integration)

### 8. Native MCP Client Implementation
* **Problem**: MCP and Plugin routes currently return empty `200 OK` arrays just to prevent crashes.
* **Solution**:
  - Implement a true JSON-RPC over stdio client in Go (`internal/mcp`).
  - Wire the MCP routes (`/mcp/{name}/auth`, `/api/skill`) to dynamically load and expose tools to the provider.

---

## Verification Criteria
- [ ] No generation runs longer than 50 turns without explicit permission.
- [ ] TUI disconnects correctly abort active non-background sessions within 1 second.
- [ ] Cost and token counts render correctly without UI parser errors.
- [ ] All tests pass: `go test ./internal/server -run 'Loop|Interrupt|State|MCP'`.
