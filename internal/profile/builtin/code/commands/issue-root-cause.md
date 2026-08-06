---
assistant: opencode
model: review
---

TAKT_PHASE: issue-root-cause
ARTIFACT_PATH: $ARTIFACTS_DIR/investigation.md

Investigate the accepted issue as a senior maintainer. This phase is evidence gathering, not implementation.

Required procedure:
1. Read the issue intake and git-state artifacts.
2. Trace the reported behavior through entry points, contracts, state transitions, tests, configuration, and recent relevant history.
3. Distinguish verified facts, hypotheses, and unknowns. Cite files and symbols for every causal claim.
4. Identify the smallest responsible boundary, affected compatibility contracts, likely regression surface, and validation commands.
5. For non-bug work, document the existing extension points and the reason the requested behavior does not exist yet.
6. Write a durable investigation to `$ARTIFACTS_DIR/investigation.md` with sections: observed behavior, expected behavior, evidence, root cause, affected scope, constraints, and validation strategy.

Return JSON only:
`{"status":"ready|blocked|failed","code":"ROOT_CAUSE_CONFIRMED|ROOT_CAUSE_UNCONFIRMED|REPOSITORY_STATE_UNEXPECTED|ISSUE_SCOPE_CONFLICT","summary":"...","evidence":["file:symbol"],"artifact_path":"$ARTIFACTS_DIR/investigation.md"}`.
