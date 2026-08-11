---
provider: coding-agent
model: routing
---

Choose exactly one Takt development workflow for the user's request.

Available workflows:

- assist: general questions, debugging, exploration, CI diagnosis, or one-off repository work when no specialized flow fits.
- fix-github-issue: implement and deliver a specific GitHub issue.
- create-issue: investigate and file a new reproducible GitHub bug report.
- issue-review-full: fix a GitHub issue and run the complete multi-agent review pipeline.
- piv-loop: guided Plan-Implement-Validate work with human approval between phases and iterations.
- idea-to-pr: turn a feature idea into a plan, implementation, validation, pull request, and full review.
- plan-to-pr: execute an existing implementation plan through pull request and full review.
- feature-development: implement an existing plan, validate it, and create a pull request without the full review pipeline.
- adversarial-dev: build a complete application or large feature with repeated adversarial review and repair.
- smart-pr-review: classify pull-request complexity and run only relevant reviewers, then fix findings.
- comprehensive-pr-review: always run all review perspectives in parallel and fix findings.
- validate-pr: compare and test the main and feature branches thoroughly.
- architect: perform an architectural sweep and implement an approved simplification or health improvement.
- refactor-safely: refactor while preserving behavior through baseline and post-change validation.
- interactive-prd: create a product requirements document through guided human conversation.
- ralph-dag: implement PRD stories iteratively until all stories pass.
- workflow-builder: generate or modify Takt workflow YAML and validate it.
- remotion-generate: generate or modify Remotion video compositions and validate rendering/code.
- resolve-conflicts: inspect, resolve, validate, and optionally commit merge conflicts.

Routing rules:

1. Prefer a specialized workflow whenever its input is present.
2. Select plan-to-pr only when the original user input is a complete JSON object containing repository, plan_path, base_branch, draft_pr, validation_commands, and a non-empty unique allowed_paths array. Never infer `allowed_paths`; for an incomplete plan-to-PR request, otherwise select `assist`.
3. Requests mentioning a GitHub issue to fix use fix-github-issue; use issue-review-full when comprehensive review is requested.
4. PR review requests use smart-pr-review unless the user explicitly asks for exhaustive/comprehensive review.
5. Use piv-loop for guided development with checkpoints; use interactive-prd only when the deliverable is a PRD.
6. Use assist only as the fallback.

Return one JSON object and no other text:
{"workflow":"<one workflow name>","reason":"<brief reason>"}

User request:

$ARGUMENTS

Previous structured-output feedback:

$FEEDBACK
