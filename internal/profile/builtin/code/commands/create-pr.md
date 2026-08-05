---
assistant: opencode
model: implementation
---

Create or update a pull request for the current branch.

Read repository instructions and artifacts. Inspect git status carefully and include only files belonging to the requested change. Never stage unrelated files or Takt run artifacts. Commit with an appropriate message when needed, push the current branch, find and fully populate the repository PR template, and create a draft PR unless the user explicitly requested ready-for-review. Reuse an existing open PR for the branch. Save the PR URL to `$ARTIFACTS_DIR/pr-url.txt` and summarize the action in `$ARTIFACTS_DIR/pr.md`.

User request:

$USER_MESSAGE
