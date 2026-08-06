---
assistant: opencode
model: review
---

TAKT_PHASE: issue-reproduction
ARTIFACT_PATH: $ARTIFACTS_DIR/reproduction.md

Create a minimal, repeatable reproduction or an explicit non-reproduction report before implementation.

Required procedure:
1. Use the issue intake and investigation artifacts.
2. Prefer an automated failing test or existing command that demonstrates the behavior without modifying production code.
3. Record exact setup, command, expected result, actual result, exit code, and relevant output.
4. If reproduction is unsafe, environment-dependent, or impossible locally, state the constraint and provide the strongest available static evidence.
5. Leave any added regression test in the checkout only when it is scoped to the issue and intentionally expected to fail until the fix.
6. Write `$ARTIFACTS_DIR/reproduction.md`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"ISSUE_REPRODUCED|ISSUE_NOT_REPRODUCED|REPRODUCTION_ENVIRONMENT_MISSING|REPRODUCTION_UNSAFE","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/reproduction.md"}`.
A verified static cause may use `ready` with `ISSUE_NOT_REPRODUCED`; explain why implementation is still justified.
