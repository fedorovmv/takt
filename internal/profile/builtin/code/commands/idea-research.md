---
assistant: coding-agent
model: review
---

TAKT_PHASE: idea-research
ARTIFACT_PATH: $ARTIFACTS_DIR/idea-research.md

Research the requested idea against the actual repository before planning.

Parse the JSON input, inspect repository instructions, architecture, public contracts, tests, similar features, and relevant history. Identify user value, current behavior, viable extension points, constraints, alternatives, explicit exclusions, and decisions that require human approval. Prefer adapting existing patterns over introducing parallel frameworks. Do not modify product code.

Write `$ARTIFACTS_DIR/idea-research.md` and return JSON only:
`{"status":"ready|blocked|failed","code":"IDEA_RESEARCH_READY|IDEA_SCOPE_AMBIGUOUS|IDEA_CONFLICTS_WITH_ARCHITECTURE|IDEA_EVIDENCE_INCOMPLETE","summary":"...","evidence":["file:symbol"],"artifact_path":"$ARTIFACTS_DIR/idea-research.md"}`.
