---
assistant: opencode
model: review
---

TAKT_PHASE: review-perspective
ARTIFACT_PATH: $ARTIFACTS_DIR/review-perspective.json

Run exactly the review perspective named in the workflow input.

Perspective contracts:
- `code`: correctness, regressions, security, concurrency, API compatibility, repository rules;
- `errors`: failure paths, cancellation, timeouts, retries, diagnostics, resource cleanup;
- `tests`: missing coverage, weak assertions, race cases, integration and compatibility risks;
- `docs`: public behavior, CLI/schema/configuration changes, examples and upgrade guidance;
- `simplicity`: unnecessary complexity, duplication, confusing abstractions, maintainability.

Required procedure:
1. Read the durable review-scope artifact and actual base-to-head diff.
2. Inspect surrounding code and tests before accepting a finding.
3. Keep only findings caused by the reviewed change. Respect explicit scope exclusions.
4. For every finding record severity, file, line or symbol, root cause, consequence, proof, and a concrete fix.
5. Do not edit files. Write `$ARTIFACTS_DIR/review-perspective.json`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"PERSPECTIVE_REVIEW_COMPLETE|PERSPECTIVE_SCOPE_INVALID|PERSPECTIVE_EVIDENCE_INCOMPLETE","summary":"...","evidence":["file:line"],"artifact_path":"$ARTIFACTS_DIR/review-perspective.json","findings":["stable finding summary"]}`.
Use an empty `findings` array when no actionable issue exists; keep `evidence` non-empty with the inspected scope.
