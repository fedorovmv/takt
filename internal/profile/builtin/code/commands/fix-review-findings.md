---
assistant: coding-agent
model: implementation
---

Implement every valid actionable finding from `$ARTIFACTS_DIR/review.md` that belongs to the requested scope.

Re-check each finding before changing code. Add tests for repaired defects, update affected documentation, and avoid unrelated cleanup. Run focused validation and write a resolution table to `$ARTIFACTS_DIR/review-fixes.md`, including evidence for rejected findings. Preserve the existing branch and PR.

User request:

$USER_MESSAGE

Previous validation feedback:

${feedback}
