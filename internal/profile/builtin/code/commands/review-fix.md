---
assistant: opencode
model: implementation
---

TAKT_PHASE: review-fix
ARTIFACT_PATH: $ARTIFACTS_DIR/review-fix-report.md

Apply only verified findings from `$ARTIFACTS_DIR/review-report.md` when the workflow input permits automatic fixes.

Required procedure:
1. Re-check each accepted finding against the current head and discard findings made stale by earlier changes.
2. Fix blockers and important findings first. Preserve scope and public contracts.
3. Add or update focused tests for every behavior-changing fix.
4. Run focused validation and inspect the resulting diff for unrelated changes.
5. If automatic fixes are disabled or a finding requires a product decision, make no speculative change and report the blocker.
6. Write `$ARTIFACTS_DIR/review-fix-report.md`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"REVIEW_FIXES_APPLIED|REVIEW_NO_FIXES_REQUIRED|REVIEW_FIX_REQUIRES_DECISION|REVIEW_FIX_VALIDATION_FAILED|REVIEW_FIX_SCOPE_DRIFT","summary":"...","evidence":["file:test"],"artifact_path":"$ARTIFACTS_DIR/review-fix-report.md"}`.
