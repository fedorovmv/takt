---
assistant: opencode
model: review
---

Run the review perspective described in the user message.

Perspective contracts:

- `code`: correctness, regressions, security, concurrency, API compatibility, repository rules;
- `errors`: failure paths, cancellation, timeouts, retries, diagnostics, resource cleanup;
- `tests`: missing coverage, weak assertions, race cases, integration and compatibility risks;
- `docs`: public behavior, CLI/schema/configuration changes, examples and upgrade guidance;
- `simplicity`: unnecessary complexity, duplication, confusing abstractions, maintainability.

Verify every finding against the actual diff and surrounding code. Return only evidence-backed actionable findings with severity and file references. State explicitly when no actionable finding exists. Do not edit source files.

Review request:

$USER_MESSAGE
