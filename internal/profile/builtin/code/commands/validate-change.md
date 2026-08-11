---
provider: coding-agent
model: review
---

Validate the current implementation against the user request and repository rules.

Inspect the diff and artifacts, run the repository's deterministic validation commands, and add focused tests when needed to prove behavior. Do not accept an agent's narrative as evidence. Record commands, results, remaining risks, and any failures in `$ARTIFACTS_DIR/validation.md`. Fix small validation-only issues directly; leave broader implementation changes for the repair step.

User request:

$ARGUMENTS
