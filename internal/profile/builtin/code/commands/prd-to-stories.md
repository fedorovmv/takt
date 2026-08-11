---
provider: coding-agent
model: review
---

Convert the PRD into an ordered durable story backlog for iterative implementation.

Read the PRD and current repository. Create small independently verifiable stories with IDs, dependencies, acceptance criteria, validation commands, and a boolean completion field. Preserve the source PRD. Write JSON to `$ARTIFACTS_DIR/prd.json` and a readable index to `$ARTIFACTS_DIR/stories.md`. If a compatible story file already exists, validate and retain its completed state.

User request:

$ARGUMENTS
