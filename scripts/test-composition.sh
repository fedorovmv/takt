#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
cp -R "$ROOT/examples/composition/." "$TMP/"
"$ROOT/bin/takt" validate "$TMP/workflow.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json >/dev/null
run_json="$($ROOT/bin/takt run "$TMP/workflow.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json)"
printf '%s' "$run_json" | grep -q '"status": "completed"'
printf '%s' "$run_json" | grep -q '"prepare__append"'
printf '%s' "$run_json" | grep -q '"batch__002"'
test "$(tr '\n' ',' < "$TMP/order.txt")" = "first,second,third,"
printf '%s\n' 'workflow composition: PASS'
