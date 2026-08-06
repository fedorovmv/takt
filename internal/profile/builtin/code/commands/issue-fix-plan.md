---
assistant: coding-agent
model: review
---

TAKT_PHASE: issue-fix-plan
ARTIFACT_PATH: $ARTIFACTS_DIR/fix-plan.md

Produce an implementation plan grounded in the accepted issue, repository state, investigation, and reproduction evidence.

The plan must contain:
- exact scope and explicit exclusions;
- files and symbols to create or change;
- contract and compatibility impact;
- ordered implementation tasks with verification after each task;
- tests that fail before and pass after the change;
- migration or rollback considerations;
- final validation commands;
- risks and recovery actions.

Reject speculative redesign that is not required by the issue. Write `$ARTIFACTS_DIR/fix-plan.md`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"FIX_PLAN_READY|PLAN_EVIDENCE_INCOMPLETE|PLAN_SCOPE_EXCEEDS_ISSUE|PLAN_CONTRACT_UNRESOLVED","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/fix-plan.md"}`.
