---
assistant: opencode
model: implementation
---

Implement the development plan below in the current repository.

The Markdown file named in the input is authoritative. Read the repository instructions and documentation before editing. Work through the plan in its existing order. Keep requirements and explanations in Markdown; do not convert the plan into a separate JSON or YAML task model.

Rules:

- implement complete working changes, not a proposal;
- update Markdown checkboxes only after the corresponding work and validation succeed;
- preserve unfinished items and explanatory text;
- inspect existing code before choosing an implementation;
- run `./.takt/profiles/code/tools/validate` before finishing;
- do not create commits unless the repository explicitly requires it;
- do not change unrelated files.

Plan:

$USER_MESSAGE

Previous validation feedback:

${feedback}
