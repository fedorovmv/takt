---
assistant: coding-agent
model: implementation
---

Build the complete requested application or large feature from the current plan and repository state.

Implement end-to-end behavior, tests, operational setup, and user documentation. Read all adversarial findings from `$ARTIFACTS_DIR` and repair them in each subsequent iteration. Prefer simple cohesive architecture, prove critical paths with deterministic checks, and record progress to `$ARTIFACTS_DIR/adversarial-implementation.md`.

User request:

$USER_MESSAGE

Previous validation feedback:

${feedback}
