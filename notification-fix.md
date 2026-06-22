# Fix Ignored Task Notifications & Child Context Loss

## Goal
Implement fixes for task notification loss (persisting completed subtasks) and enable parent context inheritance for subagents.

## Tasks
- [ ] Task 1: Update `QueueSyntheticMessage` in `server.go` → Verify: `resumeAssistantID` not used in `startOrQueue`
- [ ] Task 2: Implement `getParentContext` in `delegate_tools.go` → Verify: Parent messages included in child prompt
- [ ] Task 3: Update `renderTaskResult` tags → Verify: Output contains `<task_result>`/`<task_error>`
- [ ] Task 4: Enhance synthetic message prompt → Verify: Prompt reads task result/error and continues request
- [ ] Task 5: Allow task result injection into completed parent → Verify: Injection succeeds even if parent is terminal
- [ ] Task 6: Run tests → Verify: `go test ./internal/server -run 'Delegate|Resume|Synthetic|Context|Notification|TaskResult' -count=20`

## Done When
- All tests pass
- Task notifications persist correctly
- Child subtasks inherit parent instructions via context
