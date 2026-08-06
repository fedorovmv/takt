---
assistant: opencode
model: review
---

TAKT_PHASE: change-validation
ARTIFACT_PATH: $ARTIFACTS_DIR/validation-report.md

Validate the current change independently from the implementing phase.

Required procedure:
1. Read workflow input, scope artifacts, implementation report, and current git diff.
2. Run every input-provided validation command plus repository-required checks. Record command, exit code, duration, and a concise result.
3. Run focused regression tests and inspect whether the original acceptance criteria are directly evidenced.
4. Check for unrelated changes, missing tests, compatibility breaks, generated-file drift, secret exposure, and incomplete documentation.
5. Do not claim success for skipped checks. Distinguish unavailable infrastructure from a failed product check.
6. Write `$ARTIFACTS_DIR/validation-report.md`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"VALIDATION_PASSED|VALIDATION_FAILED|VALIDATION_ENVIRONMENT_MISSING|ACCEPTANCE_NOT_PROVEN|DIFF_SCOPE_VIOLATION","summary":"...","evidence":["command => result"],"artifact_path":"$ARTIFACTS_DIR/validation-report.md"}`.
