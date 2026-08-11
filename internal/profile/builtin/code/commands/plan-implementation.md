---
provider: coding-agent
model: implementation
---

TAKT_PHASE: plan-implementation
ARTIFACT_PATH: $ARTIFACTS_DIR/implementation-report.md

Execute the confirmed plan without silently changing its scope.

Read plan confirmation, original plan, git-state, repository instructions, and acceptance criteria. Implement tasks in order; run focused checks after each task; preserve compatibility; add tests; document any unavoidable deviation and stop when it changes approved scope. Inspect the final diff for unrelated files and secrets. Write `$ARTIFACTS_DIR/implementation-report.md`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"PLAN_IMPLEMENTED|PLAN_IMPLEMENTATION_BLOCKED|PLAN_DEVIATION_REQUIRES_APPROVAL|PLAN_TEST_FAILED|PLAN_CONTRACT_BROKEN","summary":"...","evidence":["file:test"],"artifact_path":"$ARTIFACTS_DIR/implementation-report.md"}`.
