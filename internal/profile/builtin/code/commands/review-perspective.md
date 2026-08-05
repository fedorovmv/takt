---
assistant: opencode
model: review
---

Review the current change from the **${inputs.perspective}** perspective.

Scoped change:

${inputs.scope}

Use the following perspective contract:

- `code`: correctness, regressions, security, concurrency, API compatibility, repository rules;
- `errors`: failure paths, cancellation, timeouts, retries, diagnostics, resource cleanup;
- `tests`: missing coverage, weak assertions, race cases, integration and compatibility risks;
- `docs`: public behavior, CLI/schema/configuration changes, examples and upgrade guidance;
- `simplicity`: unnecessary complexity, duplication, confusing abstractions, maintainability.

Verify every finding against the actual diff and surrounding code. Write only evidence-backed actionable findings to `$ARTIFACTS_DIR/review-${inputs.perspective}.md`, including severity and file references. State explicitly when no actionable finding exists. Do not edit source files.
