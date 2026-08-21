---
provider: coding-agent
model: implementation
---

Create or update a pull request for the current branch.

Use the current execution workspace (`$TAKT_WORKSPACE`) as the repository root
for all repository reads, writes, searches, and git/SCM commands. Do not inspect,
copy files to, or modify the control checkout, sibling workspaces, or previous
runs. Only the explicit `$ARTIFACTS_DIR` paths below may be written outside the
execution workspace. Read repository instructions and artifacts. Inspect git status
carefully and include only files belonging to the requested change. Never stage
unrelated files or Takt run artifacts. Commit with an appropriate message when
needed, push the current branch, find and fully populate the repository PR
template, and create a draft PR by executing `gh pr create` unless the user
explicitly requested ready-for-review. Reuse an existing open PR for the
branch only after checking it with `gh pr view`/`gh pr list`; never fabricate a
PR URL or replace the SCM call with hand-written placeholder artifacts. Save
the URL returned by the SCM command to `$ARTIFACTS_DIR/pr-url.txt` and
summarize the action in `$ARTIFACTS_DIR/pr.md`.

This phase publishes the already-validated implementation; it is not an
implementation or repair phase. Do not edit, reconstruct, or re-implement
product files. If the execution workspace has no product diff relative to its
base commit, stop with a clear failure and do not write a placeholder PR URL.

User request:

$ARGUMENTS
