---
assistant: coding-agent
model: review
---

Synthesize the governed reviewer results below into one deduplicated review.

${nodes.reviews.output}

Verify every finding against the current code, discard unsupported or duplicate claims, reconcile conflicts, and classify each retained item by severity and required action. Write the authoritative result to `$ARTIFACTS_DIR/review.md`. End with a clear verdict: READY, FIX_REQUIRED, or BLOCKED. Do not edit source files.
