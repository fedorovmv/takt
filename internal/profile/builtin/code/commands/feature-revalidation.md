---
provider: coding-agent
model: review
---

TAKT_PHASE: feature-revalidation
ARTIFACT_PATH: $ARTIFACTS_DIR/revalidation.md
CURRENT_WORKSPACE: $TAKT_WORKSPACE

Revalidate the repaired implementation independently inside the current workspace boundary `$TAKT_WORKSPACE`. Read `$ARTIFACTS_DIR/validation.md` and `$ARTIFACTS_DIR/review-fixes.md`, verify the original request and focused tests, and do not edit files. Record any findings and evidence as Markdown in `$ARTIFACTS_DIR/revalidation.md`, with exactly one anchored control line and no other line beginning `verdict:`:

`verdict: PASS` or `verdict: REPAIR` or `verdict: BLOCKED`

Original request:

$ARGUMENTS
