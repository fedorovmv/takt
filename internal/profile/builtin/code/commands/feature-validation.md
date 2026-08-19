---
provider: coding-agent
model: review
---

TAKT_PHASE: feature-validation
ARTIFACT_PATH: $ARTIFACTS_DIR/validation.md
CURRENT_WORKSPACE: $TAKT_WORKSPACE

Validate the current implementation independently inside the current workspace boundary `$TAKT_WORKSPACE`. Review the original request, the implementation, and its focused tests. Keep validation bounded: inspect the diff, run the repository validation command, and add at most one focused probe only when it confirms a concrete finding. Do not perform exhaustive ad-hoc experiments or create large fixtures. Put every scratch file in a directory created by `mktemp -d` outside `$TAKT_WORKSPACE`, and make every shell `cd` fail closed before creating files. Do not edit product files or leave scratch files in the workspace. Record findings and evidence promptly as Markdown in `$ARTIFACTS_DIR/validation.md`; do not postpone the artifact for optional exploration. The artifact must contain exactly one anchored control line and no other line beginning `verdict:`:

`verdict: PASS` or `verdict: REPAIR` or `verdict: BLOCKED`

`PASS` means there are no valid actionable findings. `REPAIR` means there are fixable in-scope defects. `BLOCKED` means a safe fix requires unavailable infrastructure, a product decision, or scope expansion. The verdict is a decision, not proof; Markdown evidence records the checks and findings. Markdown evidence is allowed outside the single control line.

Original request:

$ARGUMENTS
