---
assistant: coding-agent
model: implementation
---

Resolve the current git merge or rebase conflicts safely.

Inspect git status, merge base, both sides of every conflict, surrounding history, tests, generated-file rules, and repository instructions. Preserve the intent of both sides where compatible; do not choose markers mechanically. Resolve all conflicts, run focused and full validation, verify no conflict markers remain, and summarize each resolution in `$ARTIFACTS_DIR/conflicts.md`. Continue or commit the merge/rebase only when safe and requested by the workflow.

User request:

$USER_MESSAGE
