---
provider: coding-agent
model: implementation
---

Implement the requested change in the current execution workspace (`$TAKT_WORKSPACE`).
Use it as the repository root for all repository reads, writes, searches, and commands.
Do not inspect or modify the control checkout, sibling workspaces, or previous runs.
Only the explicit `$ARTIFACTS_DIR` paths below may be written outside the execution workspace.

Read AGENTS.md and the user request. Read `$ARTIFACTS_DIR/plan.md`,
`$ARTIFACTS_DIR/investigation.md`, or a supplied plan file only when it exists. If an
optional artifact is absent, proceed from the user request; do not search other
workspaces or evaluation runs for a substitute. Work through the available plan
completely, keep the change focused, add or update tests, and preserve compatibility
unless the plan explicitly changes it. Run focused checks while working. Write a
concise implementation record to `$ARTIFACTS_DIR/implementation.md`.

User request:

$ARGUMENTS

Previous validation feedback:

$FEEDBACK
