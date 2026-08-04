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
  --replace \
  --json > "$tmp/result.json"

./bin/takt-route-eval-assert "$tmp/result.json"
[[ -f "$tmp/eval/report.json" ]]
./bin/takt eval report "$tmp/eval" --json >/dev/null
