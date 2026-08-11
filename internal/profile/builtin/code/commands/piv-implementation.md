---
provider: coding-agent
model: implementation
---

TAKT_PHASE: piv-implementation
ARTIFACT_PATH: $ARTIFACTS_DIR/implementation-report.md

Implement the approved PIV plan with traceable checkpoints.

Verify plan approval and Git state, execute tasks in order, run focused checks, update tests and documentation, inspect diff scope, and record deviations. Human review feedback is authoritative only when it remains inside approved scope. Write `$ARTIFACTS_DIR/implementation-report.md`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"PIV_IMPLEMENTED|PIV_IMPLEMENTATION_BLOCKED|PIV_SCOPE_CHANGE_REQUIRED|PIV_TEST_FAILED","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/implementation-report.md"}`.
