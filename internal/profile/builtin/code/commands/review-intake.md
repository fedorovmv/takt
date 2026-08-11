---
provider: coding-agent
model: review
---

TAKT_PHASE: review-intake
ARTIFACT_PATH: $ARTIFACTS_DIR/review-scope.json

Establish the exact pull-request review scope before any reviewer runs.

Parse the JSON input and fetch PR metadata, base/head SHAs, changed files, commit list, linked issue, CI status, description, and repository instructions. Verify the requested repository and PR. Detect stale base, merge conflicts, generated or vendored files, explicit exclusions, and whether fixes are permitted. Write `$ARTIFACTS_DIR/review-scope.json`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"REVIEW_READY|PR_NOT_FOUND|PR_REPOSITORY_MISMATCH|PR_BASE_STALE|PR_CONFLICTED|REVIEW_SCOPE_EMPTY","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/review-scope.json"}`.
