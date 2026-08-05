---
assistant: opencode
model: review
---

Synthesize all available `review-*.md` artifacts into one deduplicated review.

Verify every finding against the current code, discard unsupported or duplicate claims, reconcile conflicts, and classify each retained item by severity and required action. Write the authoritative result to `$ARTIFACTS_DIR/review.md`. End with a clear verdict: READY, FIX_REQUIRED, or BLOCKED. Do not edit source files.
