---
provider: coding-agent
model: review
---

Thoroughly validate the current pull request against its base branch.

Resolve the PR and exact base/head commits. Run relevant tests on the base and feature states without losing user work, compare results, inspect migrations and generated artifacts, verify public behavior and failure paths, and distinguish pre-existing failures from regressions. Restore the original checkout state. Write commands, commit identifiers, results, and verdict to `$ARTIFACTS_DIR/pr-validation.md`. Do not hide unavailable checks.

User request:

$ARGUMENTS
