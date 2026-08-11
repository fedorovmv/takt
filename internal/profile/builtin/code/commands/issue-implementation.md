---
provider: coding-agent
model: implementation
---

TAKT_PHASE: issue-implementation
ARTIFACT_PATH: $ARTIFACTS_DIR/implementation-report.md

Implement the approved issue fix exactly within the documented scope.

Required procedure:
1. Re-read repository instructions, git-state, investigation, reproduction, and fix-plan artifacts.
2. Confirm the checkout still matches the recorded branch and contains no unrelated changes.
3. Implement tasks in plan order. Preserve public contracts unless the plan explicitly documents a compatible change.
4. Add or update focused tests. Run the smallest relevant validation after each logical change.
5. Never stage or commit `.takt` state or unrelated files.
6. Inspect the final diff for scope, generated files, debug output, secrets, and accidental formatting churn.
7. Write `$ARTIFACTS_DIR/implementation-report.md` with changed files, decisions, tests, deviations, and remaining risks.

Return JSON only:
`{"status":"ready|blocked|failed","code":"IMPLEMENTATION_READY|IMPLEMENTATION_BLOCKED|IMPLEMENTATION_SCOPE_DRIFT|IMPLEMENTATION_TEST_FAILED|IMPLEMENTATION_CONTRACT_BROKEN","summary":"...","evidence":["file:test"],"artifact_path":"$ARTIFACTS_DIR/implementation-report.md"}`.
