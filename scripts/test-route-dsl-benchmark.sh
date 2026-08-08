#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

mkdir -p bin
go build -o bin/takt ./cmd/takt
go build -o bin/takt-fake-pi ./cmd/takt-fake-pi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/cases" "$tmp/workspace"
cp examples/route-dsl-e2e/route-tool "$tmp/workspace/route-tool"
chmod +x "$tmp/workspace/route-tool"
printf '%s\n' 'HTTP -> transform -> target' > "$tmp/cases/one.md"
printf '%s\n' 'HTTP error mapping -> target' > "$tmp/cases/two.md"
cat > "$tmp/cases.yaml" <<'CASES'
apiVersion: takt/evaluation/v1alpha1
kind: CaseManifest
cases:
  one:
    category: smoke
    difficulty: basic
    source: contract
  two:
    category: errors
    difficulty: basic
    source: contract
CASES
cat > "$tmp/config.yaml" <<CFG
apiVersion: takt/v1alpha1
kind: Config
default_assistant: pi
models:
  route-model:
    provider: openai
    id: fake-route-model
assistants:
  pi:
    type: pi
    binary: $root/bin/takt-fake-pi
    args: ["--fake-case", "route-dsl"]
    session_dir: .takt/pi-sessions
    project_trust: approve
    max_output_bytes: 1048576
CFG
cat > "$tmp/matrix.yaml" <<MATRIX
apiVersion: takt/evaluation/v1alpha1
kind: EvaluationMatrix
metadata:
  name: route-dsl-contract
benchmark:
  id: route-dsl-contract-v1
  baseline_strategy: baseline-direct
  cases: $tmp/cases
  case_manifest: $tmp/cases.yaml
  workspace_template: $tmp/workspace
  repeat: 2
  quality_node: full-validation
  generation_node: implement
  validator:
    id: synthetic-route-tool
    version: "1"
    path: $tmp/workspace/route-tool
strategies:
  - id: baseline-direct
    workflow: $root/examples/route-dsl-benchmark/strategies/baseline-direct.yaml
    config: $tmp/config.yaml
  - id: feedback-repair
    workflow: $root/examples/route-dsl-benchmark/strategies/feedback-repair.yaml
    config: $tmp/config.yaml
  - id: inspect-feedback
    workflow: $root/examples/route-dsl-benchmark/strategies/inspect-feedback.yaml
    config: $tmp/config.yaml
gates:
  - strategy: feedback-repair
    final_success_rate_min: 1
    unstable_cases_max: 0
  - strategy: inspect-feedback
    final_success_rate_min: 1
    unstable_cases_max: 0
MATRIX

./bin/takt eval benchmark "$tmp/matrix.yaml" --output "$tmp/results" --replace --json > "$tmp/benchmark.stdout"
[[ -f "$tmp/results/benchmark.json" ]]
grep -Fq '"passed": true' "$tmp/results/benchmark.json"
grep -Fq '"candidate_only_valid": 4' "$tmp/results/benchmark.json"
grep -Fq '"average_time_to_valid_ms"' "$tmp/results/benchmark.json"
grep -Fq '"diagnostics_by_fingerprint"' "$tmp/results/strategies/feedback-repair/report.json"
./bin/takt eval report "$tmp/results" --json > "$tmp/report.json"
grep -Fq '"report_version": "takt-evaluation-matrix/v1alpha1"' "$tmp/report.json"

./bin/takt eval compare "$tmp/results/strategies/baseline-direct" "$tmp/results/strategies/feedback-repair" --json > "$tmp/compare.json"
grep -Fq '"candidate_only_valid": 4' "$tmp/compare.json"

# A deliberately impossible gate must return non-zero while preserving benchmark.json.
awk '
  { print }
  !done && $0 ~ /final_success_rate_min: 1/ { print "    success_at_1_min: 1"; done=1 }
' "$tmp/matrix.yaml" > "$tmp/failing-matrix.yaml"
if ./bin/takt eval benchmark "$tmp/failing-matrix.yaml" --output "$tmp/failing" --replace --json >"$tmp/failing.stdout" 2>"$tmp/failing.stderr"; then
  echo "expected evaluation gate failure" >&2
  exit 1
fi
[[ -f "$tmp/failing/benchmark.json" ]]
grep -Fq '"passed": false' "$tmp/failing/benchmark.json"

echo "Route DSL strategy benchmark: PASS"
