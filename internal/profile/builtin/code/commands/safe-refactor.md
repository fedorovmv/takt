---
provider: coding-agent
model: implementation
---

Perform the approved refactoring while preserving externally observable behavior.

Read the baseline validation evidence and plan. Make small reviewable steps, keep public contracts stable, run type checks/tests after each material step, and add characterization tests where behavior was implicit. Write the mapping from old structure to new structure and validation evidence to `$ARTIFACTS_DIR/refactor.md`.

User request:

$ARGUMENTS

Previous validation feedback:

$FEEDBACK
