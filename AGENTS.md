# Agent Instructions

- Treat `user_intentions_and_findings.md` as required project direction context.
- **Prompt scope:** never use broad prompts. Agent prompts must be as specific as possible: exact files, exact commands, exact output schema, finite scope. Avoid open-ended wording such as `if needed`, `explore broadly`, or `check anything relevant`.
- **Tool exclusions:** ignore Morph/Morph Edit and Supermemory; do not use or rely on `morph*` tools or Supermemory for this project.
- **Decision authority:** for major, architectural, destructive, detrimental, or hard-to-reverse work, only the `plan` subagent may decide direction.
- All other agents may only:
  - report findings/evidence,
  - answer scoped research questions,
  - execute an already-approved `plan` subagent task card,
  - verify/review implementation against that plan.
- Do not let coder/reviewer/specialist/explorer agents choose architecture, scope, sequencing, removals, migrations, or irreversible behavior changes.
- Do not implement those decisions until the `plan` subagent returns an executable plan/task card.
- Reference this rule before and during any such decision.
- Minor local fixes, reads, searches, formatting, and non-architectural validation may proceed directly.
