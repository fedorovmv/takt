---
assistant: opencode
model: implementation
---

Run one Ralph implementation iteration.

Read the PRD, story state, progress log, repository instructions, and current code from disk. Select exactly one highest-priority unfinished story whose dependencies are complete. Implement it fully, add tests, run deterministic validation, and update the durable story state only after validation passes. Commit only when the workflow or repository requires it. If every story is complete and the full validation suite passes, end your output with `RALPH_DONE`; otherwise end with `STORY_COMPLETE: <story id>`. Record progress in `$ARTIFACTS_DIR/ralph-progress.md` or the PRD's existing progress file.

User request:

$USER_MESSAGE
