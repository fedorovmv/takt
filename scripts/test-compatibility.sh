#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
mkdir -p bin
go build -o bin/takt ./cmd/takt

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

./bin/takt compatibility matrix --json >"$tmp/matrix.json"
./bin/takt compatibility fields --json >"$tmp/fields.json"
./bin/takt compatibility schema --json >"$tmp/schema.json"

grep -q 'takt-compatibility/v1' "$tmp/matrix.json"
grep -q 'takt-schema-subset/v1' "$tmp/schema.json"
grep -q '"field": "output_format"' "$tmp/fields.json"
grep -q '"field": "native_hooks"' "$tmp/fields.json"
grep -q '"decision": "defer"' "$tmp/fields.json"

cat >"$tmp/current.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
assistants:
  coding:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: [fake]
    capabilities: [tool_control]
YAML
./bin/takt compatibility check --workspace "$tmp" --config "$tmp/current.yaml" --strict --json >"$tmp/current-report.json"
grep -q '"status": "ready"' "$tmp/current-report.json"

cat >"$tmp/legacy.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
assistants:
  legacy:
    type: process
    protocol: takt-assistant/v1alpha1
    argv: [fake]
YAML
if ./bin/takt compatibility check --workspace "$tmp" --config "$tmp/legacy.yaml" --strict --json >"$tmp/legacy-report.json" 2>&1; then
  echo "expected strict compatibility check to reject deprecated v1alpha1 process protocol" >&2
  exit 1
fi
grep -q 'deprecated' "$tmp/legacy-report.json"

echo 'compatibility contract: PASS'
