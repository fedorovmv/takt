---
assistant: opencode
model: routing
---

Classify the current change using `$ARTIFACTS_DIR/review-scope.md` and the actual diff. Return only the JSON requested by the workflow.

The `reviewers` array must contain unique values in execution order:

- always include `code`;
- include `errors` for failure paths, concurrency, I/O, cancellation, timeout, retry, or error-handling changes;
- include `tests` for product-code changes;
- include `docs` for public behavior, CLI, schema, configuration, or other user-facing changes;
- include `simplicity` for nontrivial structural changes.
