# Message Ordering Parity Matrix

This document maps the lifecycle events that mutate session state in the TypeScript implementation to their Go counterparts, indicating ordering guarantees and any identified gaps that lead to TOCTOU or monotonicity bugs.

| Lifecycle Event | TS Write Path (file:line) | TS Event Emitted | TS Ordering / Seq. Guarantee | Go Symbol / File:line | Go Event Emitted | Go Ordering Guarantee | GAP Verdict | Bug Linkage |
|---|---|---|---|---|---|---|---|---|
| User message append | `message-v2.ts:445-452` (`AppendUserMessageWithVariant`) | `Session.Event.MessageUpdated` | DB insert → Event publish after DB write (sequential per request) | `store.go:311-338` (`AppendUserMessage`) | `event.NewMessageUpdated` | Store append guarded by RWMutex, sequential per call | match | — |
| Assistant message create (step‑start) | `processor.ts:673-682` (`ensureV2AssistantMessage` publishes `SessionEvent.Step.Started`) | `Session.Event.Step.Started` | Published before any part writes, uses monotonic `MessageID.ascending()` | `store.go:372-418` (`NewAssistantMessage`) + `store.go:641-656` (`AppendStepStart`) + `agent_loop.go:183-190` per-turn publish | `event.NewMessagePartUpdated` for persisted step-start parts | Store append guarded by RWMutex, sequential per turn | match | resolved – per-turn AppendStepStart + immediate NewMessagePartUpdated keeps step-start parts aligned with step-start events |
| Text delta (stream) | `processor.ts:784-796` (`text-delta` case publishes `SessionEvent.Text.Delta`) | `Session.Event.Text.Delta` + `MessageV2.parts` DB delta write | Append delta via `MessageV2.parts` then event publish; order relies on stream order | `store.go:440-475` (`AppendTextDelta`) | `event.NewMessagePartDelta` + `event.NewMessagePartUpdated` | Store delta guarded by RWMutex, sequential per call | match | — |
| Tool part create → running → completed | `processor.ts:468-552` (`tool-call` case publishes tool events) | Multiple tool events per call | Upserts by `callID` ensure a single part transition; events emitted after DB update | `store.go:549-636` (`AppendToolPart`) | `event.NewSessionNextToolCalled`, `...Success`, `...Failed` | Upsert guarded by lock; timestamp from `nextMS()` ensures monotonic per part | match | — |
| Step finish (assistant turn end) | `processor.ts:693-729` (`step-finish` case) publishes `SessionEvent.Step.Ended` | `Session.Event.Step.Ended` | DB update then event publish; uses `nextMS()` for timestamps | `store.go:638-658` (`AppendStepFinish`) | `event.NewSessionNextStepEnded` | Same lock ordering; monotonic timestamps | match | — |
| Message removal | `session.ts:886-889` (`removeMessage`) | `Session.Event.MessageRemoved` | Publish before deletion, immediate effect | `store.go:355-369` (`Delete`) | No direct event | N/A | missing – TS emits removal event, Go does not | missing |
| Part removed | `session.ts:893-904` (`removePart`) | `Session.Event.PartRemoved` | Publish before part deletion | `store.go:355-369` (`Delete`) | No direct part‑remove event in Go | partial – Go lacks explicit part removal function | partial |
| Prompt admission (queue) | — | `Session.Event.SessionNextPromptAdmitted` with `admittedSeq` | `s.admitSeq` monotonic per session, protected by `sesMu` lock | `generation.go:141-144` (admitSeq increment) | No direct admittedSeq persisted in store | partial – admittedSeq not persisted, only emitted; race possible if events missed | partial | TOCTOU on admission vs processing; ties to finding #1 wrong sequence/monotonicity |
| Abort / interrupt handling | `processor.ts:917-925` (`halt`) publishes `Session.Event.Error` and may publish `Session.Event.Step.Failed` | Error and step‑failed events | Uses abort flag, emits events before cleanup | `agent_loop.go:534-538` (context cancellation select) | `event.NewSessionNextStepFailed` | match | — |

## Sad Paths (selected)

1. **Concurrent tool completion + interrupt** – tool finishes after session abort, causing out‑of‑order events.
2. **admittedSeq non‑monotonic** – rapid re‑prompt can reset admittedSeq, breaking monotonicity.
3. **Out‑of‑order part broadcast vs store insertion** – part update emitted before DB records the part.
4. **Part update after removed message/part** – stale updates occur when part is updated after its message/part deletion.



## Ranked Gap List (tied to Finding #1)
1. **Resolved: step‑start part/event drift** – per-turn AppendStepStart + immediate NewMessagePartUpdated keeps step-start parts aligned with step-start events.
2. **Tool‑completion race after abort** – Out‑of‑order tool success/failure can corrupt the observable part timeline.
3. **AdmittedSeq reset on restart** – TOCTOU between admission and processing may drop prompts.
4. **Message removal event absence** – Stale messages may linger in client state.

## Decisions deferred to plan

- Append‑only event log: strongest replay/cursor ordering; larger migration.
- Persisted per‑session message/part sequence fields: smaller store diff; weaker event replay.
- Centralized event emitter after store mutation: simplest sequencing surface; still needs persistence for restart/replay.
