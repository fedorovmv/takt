---
assistant: coding-agent
model: implementation
---

TAKT_PHASE: ralph-story
ARTIFACT_PATH: $ARTIFACTS_DIR/ralph-progress.json

Execute exactly one next-ready story from the Ralph backlog in a fresh session.

Select the first incomplete story whose dependencies are complete. Re-verify its assumptions, implement only its scope, add tests, run its focused validation, and update the durable backlog/progress artifacts atomically. Never mark a story complete without evidence. If every story is complete, make no product changes.

Write `$ARTIFACTS_DIR/ralph-progress.json` and return JSON only:
`{"status":"ready|blocked|failed","code":"RALPH_STORY_COMPLETED|RALPH_ALL_DONE|RALPH_STORY_BLOCKED|RALPH_STORY_VALIDATION_FAILED|RALPH_BACKLOG_CHANGED","summary":"...","evidence":["story-id:test"],"artifact_path":"$ARTIFACTS_DIR/ralph-progress.json","done":true|false}`.
