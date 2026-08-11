---
provider: coding-agent
model: implementation
---

Generate or modify a Takt workflow and its Markdown commands for the user's project.

Read `skills/takt/SKILL.md`, current Takt schemas, profile examples, and repository requirements. Preserve the single-DAG runtime model. Create complete YAML and commands, use composition where useful, include deterministic validation and approvals, and avoid unsupported fields. Run `takt validate` for the generated workflow and fix every reported problem. Write a usage note beside the workflow. Finally write the validated workflow path as the only line of `$ARTIFACTS_DIR/workflow-path.txt`.

User request:

$ARGUMENTS

Previous validation feedback:

$FEEDBACK
