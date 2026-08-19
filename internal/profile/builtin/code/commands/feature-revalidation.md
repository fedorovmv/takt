---
provider: coding-agent
model: review
---

TAKT_PHASE: feature-revalidation
ARTIFACT_PATH: $ARTIFACTS_DIR/revalidation.md
CURRENT_WORKSPACE: $TAKT_WORKSPACE

Revalidate the repaired implementation independently inside the current workspace boundary `$TAKT_WORKSPACE`. Read `$ARTIFACTS_DIR/validation.md` and `$ARTIFACTS_DIR/review-fixes.md`, verify the original request and focused tests, and keep checks bounded to the repository validation command plus at most one focused probe for a concrete finding. Do not perform exhaustive ad-hoc experiments or create large fixtures. Put every scratch file in a directory created by `mktemp -d` outside `$TAKT_WORKSPACE`, and make every shell `cd` fail closed before creating files. Do not trust the repair narrative: independently verify the current workspace, do not edit product files, and leave no scratch files in it. Record findings and evidence promptly as Markdown in `$ARTIFACTS_DIR/revalidation.md`; do not postpone the artifact for optional exploration. The artifact must contain exactly one anchored control line and no other line beginning `verdict:`:

`verdict: PASS` or `verdict: REPAIR` or `verdict: BLOCKED`

`PASS` means there are no valid actionable findings. `REPAIR` means there are fixable in-scope defects. `BLOCKED` means a safe fix requires unavailable infrastructure, a product decision, or scope expansion. The verdict is a decision, not proof; Markdown evidence records the independent checks and findings. Markdown evidence is allowed outside the single control line.

Original request:

$ARGUMENTS
