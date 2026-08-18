---
provider: coding-agent
model: review
---

TAKT_PHASE: feature-validation
ARTIFACT_PATH: $ARTIFACTS_DIR/validation.md
CURRENT_WORKSPACE: $TAKT_WORKSPACE

Validate the current implementation independently inside the current workspace boundary `$TAKT_WORKSPACE`. Review the original request, the implementation, and its focused tests. Do not edit files. Write exactly one non-empty line to `$ARTIFACTS_DIR/validation.md`:

`verdict: PASS` or `verdict: REPAIR` or `verdict: BLOCKED`

Original request:

$ARGUMENTS
