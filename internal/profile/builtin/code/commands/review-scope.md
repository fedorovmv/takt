---
assistant: opencode
model: review
---

Determine the exact review scope for the current pull request or working-tree change.

Resolve the PR and base branch, inspect the complete diff, commits, changed public interfaces, tests, documentation, and repository instructions. Record the base/head identifiers, changed files, risk areas, and validation evidence in `$ARTIFACTS_DIR/review-scope.md`. Do not modify source files.

User request:

$USER_MESSAGE
