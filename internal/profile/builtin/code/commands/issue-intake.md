---
assistant: coding-agent
model: review
---

TAKT_PHASE: issue-intake
ARTIFACT_PATH: $ARTIFACTS_DIR/issue-intake.json

Establish an exact, traceable contract for a GitHub issue workflow.

Required procedure:
1. Parse the JSON input and require repository plus either `issue_number` or `issue_url`.
2. Fetch the issue through `gh issue view` and verify that the returned repository and number match the input.
3. Capture title, body, labels, state, acceptance criteria, linked issues or PRs, explicit exclusions, and ambiguity that could change implementation scope.
4. Search for an existing open PR or branch that already addresses the issue.
5. Do not inspect implementation details beyond what is needed to classify the request; do not modify files.
6. Write the normalized source record to `$ARTIFACTS_DIR/issue-intake.json`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"ISSUE_READY|ISSUE_NOT_FOUND|ISSUE_REPOSITORY_MISMATCH|ISSUE_ALREADY_ADDRESSED|ISSUE_INPUT_AMBIGUOUS","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/issue-intake.json"}`.
