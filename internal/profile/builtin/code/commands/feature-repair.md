---
provider: coding-agent
model: implementation
---

TAKT_PHASE: feature-repair
ARTIFACT_PATH: $ARTIFACTS_DIR/review-fixes.md
CURRENT_WORKSPACE: $TAKT_WORKSPACE

Read `$ARTIFACTS_DIR/validation.md` and re-check every finding in the current workspace boundary `$TAKT_WORKSPACE` against the original request. Apply one bounded repair only: change files inside the requested scope, add focused tests for repaired behavior, and run the relevant checks. Do not broaden scope and do not claim or perform a second repair opportunity. Write the findings and applied changes to `$ARTIFACTS_DIR/review-fixes.md`.

Original request:

$ARGUMENTS
