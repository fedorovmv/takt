---
assistant: coding-agent
model: review
---

TAKT_PHASE: plan-intake
ARTIFACT_PATH: $ARTIFACTS_DIR/plan-confirmation.md

Confirm an existing implementation plan against the current repository before executing it.

Load `plan_path` from the JSON input. Verify the file exists, extract scope, exclusions, tasks, affected contracts, validation commands, and acceptance criteria. Re-check every referenced file, symbol, dependency, and architectural assumption against the current checkout. Identify stale research and conflicts with repository instructions or current Git state. Do not implement changes.

Write `$ARTIFACTS_DIR/plan-confirmation.md` and return JSON only:
`{"status":"ready|blocked|failed","code":"PLAN_CONFIRMED|PLAN_NOT_FOUND|PLAN_STALE|PLAN_SCOPE_AMBIGUOUS|PLAN_CONTRACT_CONFLICT","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/plan-confirmation.md"}`.
