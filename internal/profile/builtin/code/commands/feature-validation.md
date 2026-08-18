---
provider: coding-agent
model: review
---

TAKT_PHASE: feature-validation
ARTIFACT_PATH: $ARTIFACTS_DIR/validation.md
CURRENT_WORKSPACE: $TAKT_WORKSPACE

Validate the current implementation independently inside the current workspace boundary `$TAKT_WORKSPACE`. Review the original request, the implementation, and its focused tests. Run available and relevant deterministic checks. Do not edit files. Record findings and evidence as Markdown in `$ARTIFACTS_DIR/validation.md`, with exactly one anchored control line and no other line beginning `verdict:`:

`verdict: PASS` or `verdict: REPAIR` or `verdict: BLOCKED`

`PASS` means there are no valid actionable findings. `REPAIR` means there are fixable in-scope defects. `BLOCKED` means a safe fix requires unavailable infrastructure, a product decision, or scope expansion. The verdict is a decision, not proof; Markdown evidence records the checks and findings. Markdown evidence is allowed outside the single control line.

Original request:

$ARGUMENTS
