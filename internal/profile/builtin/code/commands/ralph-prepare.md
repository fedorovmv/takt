---
assistant: opencode
model: review
---

TAKT_PHASE: ralph-prepare
ARTIFACT_PATH: $ARTIFACTS_DIR/ralph-backlog.json

Prepare a bounded, dependency-aware Ralph backlog from the JSON input or referenced PRD.

Read repository instructions and current code. Convert the request into independently verifiable stories with stable IDs, dependencies, scope, affected files, acceptance criteria, validation commands, and completion state. Order stories topologically and cap them at `max_stories`. Reject cycles and stories too large for one fresh agent session. Write `$ARTIFACTS_DIR/ralph-backlog.json`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"RALPH_BACKLOG_READY|RALPH_PRD_NOT_FOUND|RALPH_STORY_CYCLE|RALPH_STORY_TOO_LARGE|RALPH_SCOPE_AMBIGUOUS","summary":"...","evidence":["story-id"],"artifact_path":"$ARTIFACTS_DIR/ralph-backlog.json"}`.
