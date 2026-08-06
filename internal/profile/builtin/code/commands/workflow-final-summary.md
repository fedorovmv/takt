---
assistant: coding-agent
model: review
---

TAKT_PHASE: workflow-final-summary
ARTIFACT_PATH: $ARTIFACTS_DIR/summary.md

Produce the final evidence-based workflow summary.

Read the original JSON input, all typed artifacts, current git state, validation report, review result, recovery report when present, and PR metadata when present. State:
- requested outcome and delivered scope;
- files and behavior changed;
- acceptance evidence and exact validation commands;
- issue and PR references;
- recovery actions taken;
- known limitations, skipped checks, and follow-ups.

Do not repeat unverified claims from earlier agents. Write `$ARTIFACTS_DIR/summary.md`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"WORKFLOW_COMPLETE|WORKFLOW_INCOMPLETE|EVIDENCE_INCONSISTENT","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/summary.md"}`.
