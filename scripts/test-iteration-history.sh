#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$ROOT/bin"
go build -o "$ROOT/bin/takt" "$ROOT/cmd/takt"

cat > "$TMP/config.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
models: {}
assistants: {}
YAML

cat > "$TMP/workflow.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: iteration-history
nodes:
  - id: converge
    loop_group:
      max_iterations: 4
      nodes:
        - id: increment
          bash: |
            n=0
            test ! -f counter || n=$(cat counter)
            n=$((n + 1))
            printf '%s' "$n" > counter
            printf '%s' "$n"
        - id: check
          depends_on: [increment]
          bash: test "$(cat counter)" -ge 3
          allow_failure: true
      until:
        node: check
        exit_code: 0
YAML

"$ROOT/bin/takt" run "$TMP/workflow.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json > "$TMP/run.json"

grep -q '"status": "completed"' "$TMP/run.json"
grep -q '"loop_iterations"' "$TMP/run.json"
count="$(grep -o '"iteration": [123]' "$TMP/run.json" | wc -l | tr -d ' ')"
test "$count" = 3
grep -q '"satisfied": false' "$TMP/run.json"
grep -q '"satisfied": true' "$TMP/run.json"
grep -q '"loop_previous"' "$TMP/run.json"

cat > "$TMP/unbounded.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: unbounded-loop
nodes:
  - id: loop
    loop_group:
      max_iterations: 65
      nodes:
        - id: check
          bash: "true"
      until:
        node: check
        exit_code: 0
YAML

if "$ROOT/bin/takt" validate "$TMP/unbounded.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json >"$TMP/unbounded.out" 2>"$TMP/unbounded.err"; then
  echo 'max_iterations=65 unexpectedly accepted' >&2
  exit 1
fi
grep -q 'max_iterations must be' "$TMP/unbounded.err"
grep -q '64' "$TMP/unbounded.err"

echo 'iteration history contract: PASS'
