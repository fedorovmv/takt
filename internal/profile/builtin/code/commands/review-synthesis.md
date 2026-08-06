---
assistant: coding-agent
model: review
---

TAKT_PHASE: review-synthesis
ARTIFACT_PATH: $ARTIFACTS_DIR/review-report.md

Synthesize all reviewer outputs into one deduplicated, evidence-backed decision.

For each proposed finding, verify the cited code and changed scope. Merge duplicates by root cause. Reject style preferences without a repository rule. Classify accepted findings as blocker, important, minor, or false positive; include file, line or symbol, consequence, proof, and concrete fix. State whether the PR is approvable and whether automatic fixing is allowed by input. Write `$ARTIFACTS_DIR/review-report.md`.

Return JSON only:
`{"status":"ready|blocked|failed","code":"REVIEW_APPROVED|REVIEW_CHANGES_REQUIRED|REVIEW_EVIDENCE_INCOMPLETE|REVIEW_SCOPE_CHANGED","summary":"...","evidence":["file:line"],"artifact_path":"$ARTIFACTS_DIR/review-report.md"}`.
