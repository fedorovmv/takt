---
assistant: opencode
model: routing
---

Classify the current change using `$ARTIFACTS_DIR/review-scope.md` and the actual diff. Return only the JSON requested by the workflow. Code review always runs. Enable error review for failure paths, concurrency, I/O, or error-handling changes; test review for product-code changes; docs review for public behavior or user-facing changes; simplicity review for nontrivial structural changes.
