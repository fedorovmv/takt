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
[ "$(tr -d '[:space:]' < "$TMP/project/.takt/profiles/code/VERSION")" = "0.6.0" ]
[ -f "$TMP/project/.takt/profiles/code/workflows/review-block.yaml" ]
[ -f "$TMP/project/.takt/profiles/code/workflows/smart-review-block.yaml" ]
[ "$(find "$TMP/project/.takt/profiles/code/workflows" -maxdepth 1 -name '*.yaml' | wc -l | tr -d '[:space:]')" = "22" ]
[ "$(find "$TMP/project/.takt/profiles/code/commands" -maxdepth 1 -name '*.md' | wc -l | tr -d '[:space:]')" = "32" ]
[ -f "$TMP/project/.takt/config.yaml" ]
grep -q 'format: markdown' "$TMP/project/.takt/profiles/code/profile.yaml"
grep -q 'preserve_path: true' "$TMP/project/.takt/profiles/code/profile.yaml"
grep -q 'name: code-router' "$TMP/project/.takt/profiles/code/workflow.yaml"
grep -q 'allowed_tools: \[\]' "$TMP/project/.takt/profiles/code/workflow.yaml"
grep -q 'output_format:' "$TMP/project/.takt/profiles/code/workflow.yaml"
grep -q '^    workflow:' "$TMP/project/.takt/profiles/code/workflow.yaml"
! grep -q '^    subworkflow:' "$TMP/project/.takt/profiles/code/workflow.yaml"
! grep -q '^worktree:' "$TMP/project/.takt/profiles/code/workflow.yaml"
grep -q 'retry_on: \[protocol\]' "$TMP/project/.takt/profiles/code/workflow.yaml"
grep -q 'parallel: true' "$TMP/project/.takt/profiles/code/workflows/review-block.yaml"
grep -q '^    workflow:' "$TMP/project/.takt/profiles/code/workflows/plan-to-pr.yaml"
grep -q 'isolation: inherit' "$TMP/project/.takt/profiles/code/workflows/plan-to-pr.yaml"
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
grep -q 'Guided Plan-Implement-Validate' <<<"$describe_json"
printf '%s\n' 'code profile catalog contract: PASS'
