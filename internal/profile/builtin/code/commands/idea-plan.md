---
assistant: coding-agent
model: review
---

TAKT_PHASE: idea-plan
ARTIFACT_PATH: $ARTIFACTS_DIR/plan.md

Create a decision-ready implementation plan from the idea research and JSON input.

Include problem and value, exact scope, explicit exclusions, chosen design and rejected alternatives, affected contracts, file-by-file tasks, tests, rollout/rollback, validation commands, risks, and acceptance mapping. Every task must point to repository evidence. Write `$ARTIFACTS_DIR/plan.md`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"IDEA_PLAN_READY|IDEA_DECISION_REQUIRED|IDEA_PLAN_SCOPE_DRIFT|IDEA_PLAN_CONTRACT_UNRESOLVED","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/plan.md"}`.
