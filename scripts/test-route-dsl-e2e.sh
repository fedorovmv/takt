#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

mkdir -p bin
go build -o bin/takt ./cmd/takt
go build -o bin/takt-fake-pi ./cmd/takt-fake-pi
go build -o bin/takt-route-e2e-assert ./internal/testsupport/routee2eassert

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
cp -R examples/route-dsl-e2e/. "$tmp/"

cat > "$tmp/config-test.yaml" <<CFG
apiVersion: takt/v1alpha1
kind: Config
models:
  route-model:
    provider: openai
    id: fake-route-model
    params:
      reasoning_effort: high
assistants:
  pi:
    type: pi
    binary: $root/bin/takt-fake-pi
    args: ["--fake-case", "route-dsl"]
    session_dir: $tmp/.takt/pi-sessions
    project_trust: approve
    max_output_bytes: 1048576
CFG

./bin/takt validate "$tmp/workflow.yaml" \
  --config "$tmp/config-test.yaml" \
  --workspace "$tmp" \
  --json >/dev/null

run_json="$(./bin/takt run "$tmp/workflow.yaml" \
  --config "$tmp/config-test.yaml" \
  --workspace "$tmp" \
  --input "$tmp/specification.md" \
  --json)"
printf '%s' "$run_json" > "$tmp/run.json"

run_id="$(./bin/takt-route-e2e-assert run "$tmp/run.json")"

[[ -f "$tmp/route.yaml" ]]
grep -q '^valid: true$' "$tmp/route.yaml"
artifacts="$tmp/.takt/runs/$run_id/artifacts"
[[ -f "$artifacts/route.yaml" ]]
[[ -f "$artifacts/validation.json" ]]
grep -q '"valid":true' "$artifacts/validation.json"
grep -q 'node.retry' "$tmp/.takt/runs/$run_id/events.jsonl"

answer_json="$(./bin/takt answer "$run_id" approve-result \
  --workspace "$tmp" \
  --value approved \
  --json)"
printf '%s' "$answer_json" > "$tmp/answer.json"
./bin/takt-route-e2e-assert answer "$tmp/answer.json"

echo 'Route DSL end-to-end: PASS'
