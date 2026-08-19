---
provider: coding-agent
model: review
---

TAKT_PHASE: feature-revalidation
ARTIFACT_PATH: $ARTIFACTS_DIR/revalidation.md
CURRENT_WORKSPACE: $TAKT_WORKSPACE

Revalidate the repaired implementation independently inside the current workspace boundary `$TAKT_WORKSPACE`. Read `$ARTIFACTS_DIR/validation.md` and `$ARTIFACTS_DIR/review-fixes.md`, verify the original request and focused tests, and keep checks bounded to the repository validation command plus at most one focused probe for a concrete finding. When the original request claims exact parity with an existing executable, protocol, output, or reference implementation, that focused probe is mandatory and must directly compare the candidate with the reference on the behavior at risk; candidate-authored tests are not a substitute. Do not perform exhaustive ad-hoc experiments or create large fixtures. Put every scratch file in a directory created by `mktemp -d` outside `$TAKT_WORKSPACE`, and make every shell `cd` fail closed before creating files. Do not trust the repair narrative: independently verify the current workspace, do not edit product files, and leave no scratch files in it. Record findings and evidence promptly as Markdown in `$ARTIFACTS_DIR/revalidation.md`; do not postpone the artifact for optional exploration. The artifact must contain exactly one control line and no other verdict declaration:

`verdict: PASS` or `verdict: REPAIR` or `verdict: BLOCKED`

`PASS` means there are no valid actionable findings. `REPAIR` means there are fixable in-scope defects. `BLOCKED` means a safe fix requires unavailable infrastructure, a product decision, or scope expansion. The verdict is a decision, not proof; Markdown evidence records the independent checks and findings. Markdown evidence is allowed outside the single control line.

The verdict keyword and value are case-insensitive, and the control line may be an ATX Markdown heading with one to six `#` characters. Before finishing, run `./.takt/profiles/code/tools/require-verdict "$ARTIFACTS_DIR/revalidation.md" revalidation` and correct the artifact if it fails.

Original request:

$ARGUMENTS
