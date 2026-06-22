# User Findings and Intentions

## Bug Findings
1. **Message Sequence:** The message sequence is wrong.
2. **Interrupt Handling:** Cannot interrupt; closing the TUI does not automatically stop the generation.
3. **MCP & Plugins:** No real MCP; plugin ports are currently hoaxes.
4. **Context Statistics & DCP:** Context statistics are all wrong. There is no DCP compression notification (or it's broken).
5. **Todos:** No todo usable.

## Intentions & Directives
1. **True Parity:** I do not want this to be a lightweight, minimal "happy path." I want it to be a true parity port.
2. **Loop Detection:** I do not want a hardcoded timeout or turn limit (`maxTurn`). I want smart/automatic loop detection and stop, matching the original TS loop detection logic.
