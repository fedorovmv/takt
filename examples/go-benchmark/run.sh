#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
example="$root/examples/go-benchmark"

: "${TAKT_BENCH_HOST:=all}"
: "${TAKT_REPEAT:=1}"
: "${TAKT_BENCH_OUTPUT:=$example/.takt/evals}"

[[ -x "$root/bin/takt" ]] || { echo "build bin/takt first" >&2; exit 1; }
[[ "$TAKT_REPEAT" =~ ^[1-9][0-9]*$ ]] || { echo "TAKT_REPEAT must be >= 1" >&2; exit 1; }

temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT

validator="$temp_dir/takt-go-benchmark-validator"
(cd "$root" && go build -o "$validator" ./examples/go-benchmark/validator)
export TAKT_GO_BENCHMARK_VALIDATOR="$validator"
export TAKT_GO_BENCHMARK_BASELINE="$example/workspace"

run_host() {
  local host="$1"
  "$root/bin/takt" eval benchmark "$example/matrix.$host.yaml" \
    --repeat "$TAKT_REPEAT" \
    --output "$TAKT_BENCH_OUTPUT/$host" \
    --replace \
    --json
}

case "$TAKT_BENCH_HOST" in
  pi|opencode) run_host "$TAKT_BENCH_HOST" ;;
  all)
    run_host pi
    run_host opencode
    ;;
  *) echo "TAKT_BENCH_HOST must be pi, opencode, or all" >&2; exit 1 ;;
esac
