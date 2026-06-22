# User Findings and Intentions

### Bug Findings
1. **Message Sequence:** The message sequence is wrong, causing TOCTOU/monotonicity issues.
2. **Interrupts:** Resolved (Parity verified). Go port has `POST /session/{id}/abort` which cancels context cooperatively. Closing TUI does not auto-abort in TS either; auto-abort on disconnect is a future divergence feature, not a parity gap.
3. **Subagent Looping:** Subagents sometimes hit an infinite loop without stopping.
4. **False Endpoints:** There is no real MCP; the plugin ports and MCP routes are currently hoaxes.
5. **Statistics & DCP:** Context statistics are completely wrong, and there is no working DCP compression notification.
6. **Todo Tooling:** There is no usable Todo feature integrated.

### Intentions & Directives
1. **True Parity:** Do not want this to be a lightweight, minimal "happy path." Demand a true 1-to-1 parity port of the original TypeScript codebase.
2. **Sad Paths:** Want a concrete plan to iron out all of these sad paths natively.
3. **Smart Loop Detection:** Explicitly DO NOT want a hardcoded `maxTurn` or timeout limit.
4. **Original TS Logic:** Want to exactly match the original TS loop detection logic, which requires smart/automatic loop detection and stopping (e.g., detecting identical repeated tool calls rather than blindly counting turns).
5. **Preserve Ideas:** Want all findings and intentions exported and preserved as a reference file for the project's direction (only own ideas).

### Source Code Locations
- **TypeScript Base Repository:** `/tmp/opencode/`
- **Go Port Repository:** `/home/thinh0704hcm/opencode-go/`
- **Cloned Plugins Repositories:** `https://opencode.ai/docs/plugins/` to retrieve plugin repos
