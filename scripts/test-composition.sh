#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
cp -R "$ROOT/examples/composition/." "$TMP/"
"$ROOT/bin/takt" validate "$TMP/workflow.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json >/dev/null
run_json="$("$ROOT/bin/takt" run "$TMP/workflow.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json)"
printf '%s' "$run_json" | grep -q '"status": "completed"'
printf '%s' "$run_json" | grep -q '"prepare"'
printf '%s' "$run_json" | grep -q '"batch"'
printf '%s' "$run_json" | grep -Fq '\"second\",\"third\"'
if printf '%s' "$run_json" | grep -q 'prepare__\|batch__'; then
  echo 'public Run state exposes expanded node IDs' >&2
  exit 1
fi
test "$(tr '\n' ',' < "$TMP/order.txt")" = "first,second,third,"
printf '%s\n' 'workflow composition: PASS'
