#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$ROOT/bin"
go build -o "$ROOT/bin/takt" "$ROOT/cmd/takt"
go build -o "$ROOT/bin/takt-fake-code-agent" "$ROOT/cmd/takt-fake-code-agent"

make_template() {
  local dir="$1"
  local force_template="$2"
  "$ROOT/bin/takt" init code --dir "$dir" --json >/dev/null
  cat > "$dir/.takt/config.yaml" <<CFG
apiVersion: takt/v1alpha1
kind: Config
default_assistant: fixture
models:
  routing:
    provider: fixture
    id: routing
  implementation:
    provider: fixture
    id: implementation
  review:
    provider: fixture
    id: review
assistants:
  fixture:
    type: process
    argv:
      - $ROOT/bin/takt-fake-code-agent
CFG
  if [ "$force_template" = yes ]; then
    cat >> "$dir/.takt/config.yaml" <<'CFG'
    env:
      FAKE_TASK_ROUTER_FORCE_TEMPLATE: "1"
CFG
  fi
  cat >> "$dir/.takt/config.yaml" <<'CFG'
    capabilities:
      - tool_policy
      - skills
      - mcp
      - sandbox_filesystem
CFG
  git -C "$dir" init -q
  git -C "$dir" config user.email fixture@example.com
  git -C "$dir" config user.name Fixture
  printf 'fixture\n' > "$dir/README.fixture"
  cat >> "$dir/.git/info/exclude" <<'EXCLUDE'
.takt/plans/
.takt/runs/
.takt/locks/
.takt/host-sessions/
.takt/notifications/
EXCLUDE
  git -C "$dir" add .
  git -C "$dir" commit -qm 'task evaluation fixture'
}

make_template "$TMP/baseline" yes
make_template "$TMP/candidate" no

cat > "$TMP/cases.yaml" <<'YAML'
apiVersion: takt/evaluation/v1alpha1
kind: TaskCaseManifest
cases:
  ordinary:
    goal: Implement an ordinary repository change
    expected_route: template
    expected_template: simple-reliable
    expected_status: completed
    labels:
      category: routing
  dynamic:
    goal: fixture dynamic audit
    expected_route: dynamic
    expected_status: completed
    labels:
      category: dynamic
  replan:
    goal: fixture dynamic replan
    expected_route: dynamic
    expected_status: completed
    min_plan_revisions: 2
    labels:
      category: replan
YAML

cat > "$TMP/matrix.yaml" <<'YAML'
apiVersion: takt/evaluation/v1alpha1
kind: TaskEvaluationMatrix
metadata:
  name: task-dynamic
benchmark:
  id: task-dynamic-v1
  baseline_strategy: force-template
  cases: cases.yaml
  repeat: 1
  profile: code
strategies:
  - id: force-template
    workspace_template: baseline
  - id: semantic-router
    workspace_template: candidate
gates:
  - strategy: semantic-router
    route_accuracy_min: 1
    final_success_rate_min: 1
    replan_expectation_rate_min: 1
    unexpected_needs_input_max: 0
    router_fallbacks_max: 0
YAML

"$ROOT/bin/takt" eval task-benchmark "$TMP/matrix.yaml" --output "$TMP/out" --replace --json > "$TMP/result.json"
"$ROOT/bin/takt" eval report "$TMP/out" --json > "$TMP/report.json"

grep -q '"report_version": "takt-task-evaluation-matrix/v1alpha1"' "$TMP/result.json"
grep -q '"passed": true' "$TMP/result.json"
grep -q '"route_accuracy": 0.3333333333333333' "$TMP/result.json"
grep -q '"route_accuracy": 1' "$TMP/result.json"
grep -q '"candidate_only_route_correct": 2' "$TMP/result.json"
grep -q '"candidate_plan_revisions": 2' "$TMP/result.json"
grep -q '"replan_expectation_rate": 1' "$TMP/result.json"
grep -q '"report_version": "takt-task-evaluation-matrix/v1alpha1"' "$TMP/report.json"

echo 'task-level dynamic evaluation: PASS'
