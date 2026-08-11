---
provider: coding-agent
model: review
---

TAKT_PHASE: piv-plan
ARTIFACT_PATH: $ARTIFACTS_DIR/plan.md

Create or revise the PIV implementation plan from the durable exploration and latest human decision.

Preserve approved choices, explicitly record rejected alternatives, map tasks to files and tests, define phase checkpoints, acceptance evidence, validation commands, risks, recovery actions, and exclusions. Never treat ambiguous feedback as approval. Write `$ARTIFACTS_DIR/plan.md`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"PIV_PLAN_READY|PIV_PLAN_NEEDS_DECISION|PIV_PLAN_SCOPE_DRIFT|PIV_PLAN_STALE","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/plan.md"}`.
