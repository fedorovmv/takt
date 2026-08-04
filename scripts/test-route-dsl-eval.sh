#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

mkdir -p bin
go build -o bin/takt ./cmd/takt
go build -o bin/takt-fake-pi ./cmd/takt-fake-pi
go build -o bin/takt-route-eval-assert ./internal/testsupport/evalassert

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/cases"
cat > "$tmp/cases/http-jq.md" <<'CASE'
Получить клиента по HTTP, преобразовать ответ через jq и отправить результат в целевую систему.
CASE
cat > "$tmp/cases/error-path.md" <<'CASE'
Получить данные по HTTP, обработать отсутствие записи контролируемой ошибкой и отправить результат далее.
CASE

cat > "$tmp/config.yaml" <<CFG
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
    session_dir: .takt/pi-sessions
    project_trust: approve
    max_output_bytes: 1048576
CFG

./bin/takt eval run examples/route-dsl-e2e/workflow.yaml \
  --config "$tmp/config.yaml" \
  --cases "$tmp/cases" \
  --workspace-template examples/route-dsl-e2e \
  --output "$tmp/eval" \
  --answer approved \
  --strategy-id fake-pi-route-feedback-v1 \
  --benchmark-id route-dsl-infrastructure \
  --quality-node full-validation \
  --generation-node implement \
  --validator-id synthetic-route-tool \
  --validator-version 1 \
  --validator-path route-tool \
  --replace \
  --json > "$tmp/result.json"

./bin/takt-route-eval-assert "$tmp/result.json"
[[ -f "$tmp/eval/report.json" ]]
./bin/takt eval report "$tmp/eval" --json >/dev/null

# Normalized case IDs must be unique before output is created.
mkdir -p "$tmp/collision-cases"
printf '%s\n' first > "$tmp/collision-cases/a b.md"
printf '%s\n' second > "$tmp/collision-cases/a+b.md"
if ./bin/takt eval run examples/route-dsl-e2e/workflow.yaml \
  --config "$tmp/config.yaml" \
  --cases "$tmp/collision-cases" \
  --workspace-template examples/route-dsl-e2e \
  --output "$tmp/collision-output" \
  --replace >"$tmp/collision.stdout" 2>"$tmp/collision.stderr"; then
  echo "expected normalized case ID collision" >&2
  exit 1
fi
grep -Fq 'case id collision' "$tmp/collision.stderr"
[[ ! -e "$tmp/collision-output" ]]

# Output must not be nested in the workspace template.
cp -R examples/route-dsl-e2e "$tmp/overlap-template"
if ./bin/takt eval run "$tmp/overlap-template/workflow.yaml" \
  --config "$tmp/config.yaml" \
  --cases "$tmp/cases" \
  --workspace-template "$tmp/overlap-template" \
  --output "$tmp/overlap-template/results" \
  --replace >"$tmp/overlap.stdout" 2>"$tmp/overlap.stderr"; then
  echo "expected workspace template/output overlap rejection" >&2
  exit 1
fi
grep -Fq 'must not overlap' "$tmp/overlap.stderr"
[[ ! -e "$tmp/overlap-template/results" ]]

echo "Route DSL evaluation isolation: PASS"
