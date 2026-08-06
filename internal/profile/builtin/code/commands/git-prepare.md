---
assistant: coding-agent
model: implementation
---

TAKT_PHASE: git-prepare
ARTIFACT_PATH: $ARTIFACTS_DIR/git-state.json

Prepare the current isolated checkout for the requested workflow without implementing product changes.

Required procedure:
1. Read `AGENTS.md` and repository-specific contribution instructions.
2. Parse the JSON workflow input from `$USER_MESSAGE` and identify repository, base branch, requested PR mode, and scope limits.
3. Inspect `git rev-parse --show-toplevel`, `git branch --show-current`, `git status --porcelain`, configured remotes, and commits relative to the requested base.
4. If Takt already placed the Run in a worktree, keep its current branch. Never switch away from a managed worktree branch.
5. Outside a managed worktree, create a dedicated branch only when the current branch is the clean requested base. Reuse an existing matching feature branch. Stop on unrelated changes or an unexpected branch.
6. Do not merge, rebase, push, commit, or modify product files in this phase.
7. Write `$ARTIFACTS_DIR/git-state.json` with repository root, branch, base branch, worktree detection, cleanliness, remote, and decision evidence.

Return JSON only:
`{"status":"ready|blocked|failed","code":"GIT_READY|GIT_DIRTY|GIT_BRANCH_MISMATCH|GIT_BASE_MISSING|GIT_REMOTE_MISSING","summary":"...","evidence":["..."],"artifact_path":"$ARTIFACTS_DIR/git-state.json"}`.
Use `ready` only when later implementation may safely modify the checkout.
