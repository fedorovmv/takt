---
assistant: coding-agent
model: review
---

TAKT_PHASE: ralph-summary
ARTIFACT_PATH: $ARTIFACTS_DIR/ralph-summary.md

Audit the final Ralph backlog, progress history, Git diff, validation evidence, and PR metadata. Confirm every story has direct acceptance evidence and all dependencies were respected. List completed stories, deviations, recovery actions, remaining work, and final validation. Write `$ARTIFACTS_DIR/ralph-summary.md`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"RALPH_COMPLETE|RALPH_INCOMPLETE|RALPH_EVIDENCE_INCONSISTENT","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/ralph-summary.md"}`.
