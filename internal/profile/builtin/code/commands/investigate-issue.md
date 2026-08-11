---
provider: coding-agent
model: review
---

Investigate the reported issue in the current repository before implementation.

Read repository instructions, fetch the GitHub issue when referenced, inspect relevant code and tests, examine recent history, and reproduce the behavior when feasible. Identify the most likely root cause, affected files, constraints, and validation strategy. Write the durable investigation to `$ARTIFACTS_DIR/investigation.md`. Do not modify product code in this step.

User request:

$ARGUMENTS
