---
provider: coding-agent
model: implementation
---

TAKT_PHASE: workflow-recovery
ARTIFACT_PATH: $ARTIFACTS_DIR/recovery-report.md

Recover a workflow phase from its explicit checkpoint failure without broadening scope.

Required procedure:
1. Read the failed checkpoint JSON supplied in the prompt, its durable artifact, original workflow input, and current git state.
2. Classify the failure as code defect, stale evidence, environment gap, Git-state conflict, validation failure, or irrecoverable scope ambiguity.
3. Apply only reversible corrections supported by existing artifacts. For code or test failures, fix and rerun focused validation. For environment or input ambiguity, do not fabricate success.
4. Preserve the original failure evidence and document every corrective action.
5. Write `$ARTIFACTS_DIR/recovery-report.md`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"RECOVERY_APPLIED|RECOVERY_NOT_POSSIBLE|RECOVERY_REQUIRES_HUMAN|RECOVERY_SCOPE_CONFLICT","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/recovery-report.md"}`.
Use `ready` only when the failed condition has been demonstrably corrected.
