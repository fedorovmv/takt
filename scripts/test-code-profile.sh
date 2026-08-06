#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/project"
cat > "$TMP/project/PLAN.md" <<'PLAN'
# Development plan

- [ ] Implement the requested change.
- [ ] Run project validation.
PLAN
"$ROOT/bin/takt" init code --dir "$TMP/project" --json >/dev/null
"$ROOT/bin/takt" validate code --workspace "$TMP/project" --json >/dev/null
[ -f "$TMP/project/.takt/profiles/code/profile.yaml" ]
[ "$(tr -d '[:space:]' < "$TMP/project/.takt/profiles/code/VERSION")" = "0.12.0" ]
[ -f "$TMP/project/.takt/profiles/code/workflows/review-block.yaml" ]
[ -f "$TMP/project/.takt/profiles/code/workflows/smart-review-block.yaml" ]
[ -f "$TMP/project/.takt/profiles/code/workflows/blocks/package.yaml" ]
[ "$(find "$TMP/project/.takt/profiles/code/workflows/blocks" -maxdepth 1 -name 'dynamic-*.yaml' | wc -l | tr -d '[:space:]')" = "7" ]
grep -q '^block_packages:' "$TMP/project/.takt/profiles/code/profile.yaml"
[ "$(find "$TMP/project/.takt/profiles/code/workflows" -maxdepth 1 -name '*.yaml' | wc -l | tr -d '[:space:]')" = "24" ]
[ "$(find "$TMP/project/.takt/profiles/code/commands" -maxdepth 1 -name '*.md' | wc -l | tr -d '[:space:]')" = "63" ]
[ -f "$TMP/project/.takt/config.yaml" ]
[ -x "$TMP/project/.takt/profiles/code/tools/review-perspectives" ]
grep -q 'format: markdown' "$TMP/project/.takt/profiles/code/profile.yaml"
grep -q 'preserve_path: true' "$TMP/project/.takt/profiles/code/profile.yaml"
grep -q 'name: code-router' "$TMP/project/.takt/profiles/code/workflow.yaml"
grep -q 'allowed_tools: \[\]' "$TMP/project/.takt/profiles/code/workflow.yaml"
grep -q 'output_format:' "$TMP/project/.takt/profiles/code/workflow.yaml"
grep -q '^    workflow:' "$TMP/project/.takt/profiles/code/workflow.yaml"
! grep -q '^    subworkflow:' "$TMP/project/.takt/profiles/code/workflow.yaml"
! grep -q '^worktree:' "$TMP/project/.takt/profiles/code/workflow.yaml"
grep -q 'retry_on: \[protocol\]' "$TMP/project/.takt/profiles/code/workflow.yaml"
grep -q '^    script:' "$TMP/project/.takt/profiles/code/workflows/review-block.yaml"
grep -q 'output_type: review-perspectives' "$TMP/project/.takt/profiles/code/workflows/review-block.yaml"
grep -q 'output_type: plan' "$TMP/project/.takt/profiles/code/workflows/idea-to-pr.yaml"
grep -q 'output_type: prd' "$TMP/project/.takt/profiles/code/workflows/interactive-prd.yaml"
grep -q 'fan_out:' "$TMP/project/.takt/profiles/code/workflows/review-block.yaml"
grep -q 'max_parallel: 5' "$TMP/project/.takt/profiles/code/workflows/review-block.yaml"
grep -q 'items_from: nodes.classify.output.reviewers' "$TMP/project/.takt/profiles/code/workflows/smart-review-block.yaml"
grep -q '^    workflow:' "$TMP/project/.takt/profiles/code/workflows/plan-to-pr.yaml"
grep -q 'isolation: inherit' "$TMP/project/.takt/profiles/code/workflows/plan-to-pr.yaml"
grep -q '^input:' "$TMP/project/.takt/profiles/code/workflows/fix-github-issue.yaml"
grep -q 'format: json' "$TMP/project/.takt/profiles/code/workflows/fix-github-issue.yaml"
grep -q 'output_type: investigation' "$TMP/project/.takt/profiles/code/workflows/fix-github-issue.yaml"
grep -q 'command: workflow-recovery' "$TMP/project/.takt/profiles/code/workflows/plan-to-pr.yaml"
grep -q 'output_type: ralph-backlog' "$TMP/project/.takt/profiles/code/workflows/ralph-dag.yaml"
grep -q 'TAKT_PHASE: issue-intake' "$TMP/project/.takt/profiles/code/commands/issue-intake.md"

grep -q '^worktree:' "$TMP/project/.takt/profiles/code/workflows/feature-development.yaml"
! grep -q '^worktree:' "$TMP/project/.takt/profiles/code/workflows/smart-pr-review.yaml"
! grep -q '^worktree:' "$TMP/project/.takt/profiles/code/workflows/resolve-conflicts.yaml"

workflows=(
  assist fix-github-issue create-issue issue-review-full piv-loop
  idea-to-pr plan-to-pr feature-development adversarial-dev smart-pr-review
  comprehensive-pr-review validate-pr architect refactor-safely interactive-prd
  ralph-dag workflow-builder remotion-generate resolve-conflicts
)
for name in "${workflows[@]}"; do
  "$ROOT/bin/takt" validate "code:$name" --workspace "$TMP/project" --json >/dev/null
  [ -f "$TMP/project/.takt/profiles/code/workflows/$name.yaml" ]
done
list_json="$($ROOT/bin/takt workflow list code --workspace "$TMP/project" --json)"
describe_json="$($ROOT/bin/takt workflow describe code:piv-loop --workspace "$TMP/project" --json)"
for name in "${workflows[@]}"; do
  grep -q "code:$name" <<<"$list_json"
done
grep -q 'Plan-Implement-Validate' <<<"$describe_json"
printf '%s\n' 'code profile catalog contract: PASS'
