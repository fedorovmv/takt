---
provider: coding-agent
model: implementation
---

Implement the requested change in the current checkout.

Read AGENTS.md, the user request, and available artifacts such as `$ARTIFACTS_DIR/plan.md`, `$ARTIFACTS_DIR/investigation.md`, or a supplied plan file. Work through the plan completely, keep the change focused, add or update tests, and preserve compatibility unless the plan explicitly changes it. Run focused checks while working. Write a concise implementation record to `$ARTIFACTS_DIR/implementation.md`.

User request:

$ARGUMENTS

Previous validation feedback:

$FEEDBACK
