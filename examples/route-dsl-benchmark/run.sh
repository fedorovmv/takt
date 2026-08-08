#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
example="$root/examples/route-dsl-benchmark"

: "${TAKT_CONFIG:=$example/config.example.yaml}"
: "${TAKT_ROUTE_VALIDATOR:?set TAKT_ROUTE_VALIDATOR to the production Route DSL validator or its takt-validation wrapper}"
: "${TAKT_BENCH_OUTPUT:=$example/.takt/evals/route-dsl-strategies}"
: "${TAKT_REPEAT:=3}"

[[ -x "$TAKT_ROUTE_VALIDATOR" ]] || { echo "validator is not executable: $TAKT_ROUTE_VALIDATOR" >&2; exit 1; }

workspace="$(mktemp -d)"
trap 'rm -rf "$workspace"' EXIT
cp -R "$example/workspace/." "$workspace/"
cp "$TAKT_ROUTE_VALIDATOR" "$workspace/route-tool"
chmod +x "$workspace/route-tool"

validator_version="$($TAKT_ROUTE_VALIDATOR --version 2>/dev/null | head -n 1 || true)"
[[ -n "$validator_version" ]] || validator_version="unknown"

matrix="$workspace/matrix.yaml"
cat > "$matrix" <<MATRIX
apiVersion: takt/evaluation/v1alpha1
kind: EvaluationMatrix
metadata:
  name: route-dsl-strategies
benchmark:
  id: route-dsl-production-shaped-25-v1
  baseline_strategy: baseline-direct
  cases: $example/cases
  case_manifest: $example/cases.yaml
  workspace_template: $workspace
  repeat: $TAKT_REPEAT
  quality_node: full-validation
  generation_node: implement
  validator:
    id: route-tool
    version: "$validator_version"
    path: $TAKT_ROUTE_VALIDATOR
strategies:
  - id: baseline-direct
    workflow: $example/strategies/baseline-direct.yaml
    config: $TAKT_CONFIG
  - id: feedback-repair
    workflow: $example/strategies/feedback-repair.yaml
    config: $TAKT_CONFIG
  - id: inspect-feedback
    workflow: $example/strategies/inspect-feedback.yaml
    config: $TAKT_CONFIG
MATRIX

"$root/bin/takt" eval benchmark "$matrix" \
  --output "$TAKT_BENCH_OUTPUT" \
  --replace \
  --json
