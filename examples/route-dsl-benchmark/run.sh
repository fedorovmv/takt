#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
example="$root/examples/route-dsl-benchmark"

: "${TAKT_CONFIG:=$example/config.example.yaml}"
: "${TAKT_ROUTE_VALIDATOR:?set TAKT_ROUTE_VALIDATOR to the production Route DSL validator or its takt-validation wrapper}"
: "${TAKT_BENCH_OUTPUT:=$example/.takt/evals/qwen-route-feedback-v1}"
: "${TAKT_REPEAT:=1}"

command -v pi >/dev/null 2>&1 || { echo "pi is not installed" >&2; exit 1; }
[[ -x "$TAKT_ROUTE_VALIDATOR" ]] || { echo "validator is not executable: $TAKT_ROUTE_VALIDATOR" >&2; exit 1; }

workspace="$(mktemp -d)"
trap 'rm -rf "$workspace"' EXIT
cp -R "$example/workspace/." "$workspace/"
cp "$TAKT_ROUTE_VALIDATOR" "$workspace/route-tool"
chmod +x "$workspace/route-tool"

validator_version="$($TAKT_ROUTE_VALIDATOR --version 2>/dev/null | head -n 1 || true)"
[[ -n "$validator_version" ]] || validator_version="unknown"

"$root/bin/takt" eval run "$example/workflow.yaml" \
  --config "$TAKT_CONFIG" \
  --cases "$example/cases" \
  --workspace-template "$workspace" \
  --output "$TAKT_BENCH_OUTPUT" \
  --strategy-id qwen-route-feedback-resume-v1 \
  --benchmark-id route-dsl-real-10-v1 \
  --quality-node full-validation \
  --generation-node implement \
  --validator-id route-tool \
  --validator-version "$validator_version" \
  --validator-path "$TAKT_ROUTE_VALIDATOR" \
  --answer approved \
  --repeat "$TAKT_REPEAT" \
  --replace \
  --json
