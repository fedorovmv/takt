---
provider: coding-agent
model: review
---

TAKT_PHASE: piv-exploration
ARTIFACT_PATH: $ARTIFACTS_DIR/exploration.md

Conduct one evidence-based exploration round for the Plan-Implement-Validate workflow.

Read the JSON request, repository instructions, existing exploration artifact, current code, tests, and prior human feedback. Update current understanding, constraints, alternatives, scope boundaries, risks, and unresolved decisions. Ask only questions whose answers materially affect implementation. Preserve settled decisions. Write `$ARTIFACTS_DIR/exploration.md`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"EXPLORATION_READY_FOR_PLAN|EXPLORATION_NEEDS_DECISION|EXPLORATION_SCOPE_CONFLICT|EXPLORATION_EVIDENCE_INCOMPLETE","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/exploration.md"}`.
