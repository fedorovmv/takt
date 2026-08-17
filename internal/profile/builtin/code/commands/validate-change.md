---
provider: coding-agent
model: review
---

Validate the current implementation in the current execution workspace (`$TAKT_WORKSPACE`)
against the user request and repository rules. Use it as the repository root for all
repository reads, writes, searches, and commands. Do not inspect or modify the control
checkout, sibling workspaces, or previous runs. Only the explicit `$ARTIFACTS_DIR`
paths below may be written outside the execution workspace.

Inspect the diff and artifacts, run the repository's deterministic validation commands, and add focused tests when needed to prove behavior. Do not accept an agent's narrative as evidence. Record commands, results, remaining risks, and any failures in `$ARTIFACTS_DIR/validation.md`. Fix small validation-only issues directly; leave broader implementation changes for the repair step.
When the request requires parity with a reference implementation or utility,
compare the exact exit code, stdout, and stderr. Any observable mismatch is a
validation failure and must not be waived as presentation-only unless the user
contract explicitly permits that normalization.

User request:

$ARGUMENTS
