---
provider: coding-agent
model: implementation
---

TAKT_PHASE: pr-finalize
ARTIFACT_PATH: $ARTIFACTS_DIR/pr.json

Finalize the current isolated branch into a traceable GitHub pull request.

Git decision tree:
1. Confirm the current branch is the recorded managed worktree or expected feature branch. Never switch branches inside a managed worktree.
2. If `git status --porcelain` contains unrelated or `.takt` files, stop with `PR_DIFF_SCOPE_VIOLATION`.
3. If validation evidence is not ready, stop with `PR_VALIDATION_MISSING`.
4. Stage only reviewed product, test, and documentation files. Create a focused commit only when uncommitted intended changes exist.
5. Push the current branch with upstream tracking. Reuse an open PR for the same head branch; otherwise create one using the repository template.
6. Use draft mode unless JSON input explicitly requests ready-for-review.
7. Include issue linkage, scope, implementation summary, exact validation evidence, risks, and explicit exclusions.
8. Write `$ARTIFACTS_DIR/pr.json` with URL, number, head, base, draft state, commit SHA, and validation links.

Return JSON only:
`{"status":"ready|blocked|failed","code":"PR_READY|PR_GIT_STATE_INVALID|PR_DIFF_SCOPE_VIOLATION|PR_VALIDATION_MISSING|PR_PUSH_FAILED|PR_CREATE_FAILED","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/pr.json"}`.
