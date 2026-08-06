#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
TAKT="$ROOT/bin/takt"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/.takt"

cat > "$WORK/.takt/config.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  limited:
    type: process
    argv: [/bin/cat]
YAML

cat > "$WORK/typo.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: typo
nodes:
  - id: work
    prompt: hello
    assistant: limited
    model: demo
    idle_timout: 10s
YAML
if "$TAKT" validate "$WORK/typo.yaml" --config "$WORK/.takt/config.yaml" --workspace "$WORK" >"$WORK/typo.out" 2>&1; then
  echo 'expected typo validation to fail' >&2
  exit 1
fi
grep -q 'did you mean "idle_timeout"' "$WORK/typo.out"

cat > "$WORK/capability.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: capability
nodes:
  - id: work
    prompt: hello
    assistant: limited
    model: demo
    denied_tools: [write]
YAML
if "$TAKT" validate "$WORK/capability.yaml" --config "$WORK/.takt/config.yaml" --workspace "$WORK" >"$WORK/capability.out" 2>&1; then
  echo 'expected capability validation to fail' >&2
  exit 1
fi
grep -q 'capability validation' "$WORK/capability.out"
grep -q 'tool_policy' "$WORK/capability.out"

cat > "$WORK/output-ref.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: output-reference
nodes:
  - id: produce
    script:
      runtime: python
      inline: |
        print('{"summary":"ok"}')
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
      additionalProperties: false
  - id: consume
    depends_on: [produce]
    bash: printf '%s' '${nodes.produce.output.summry}'
YAML
if "$TAKT" validate "$WORK/output-ref.yaml" --config "$WORK/.takt/config.yaml" --workspace "$WORK" >"$WORK/output-ref.out" 2>&1; then
  echo 'expected output reference validation to fail' >&2
  exit 1
fi
grep -q 'output path "summry" is not declared' "$WORK/output-ref.out"

cat > "$WORK/render.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: strict-renderer
nodes:
  - id: produce
    script:
      runtime: python
      inline: |
        print('{"summary":"ok","empty":""}')
    output_format:
      type: object
      properties:
        summary:
          type: string
        empty:
          type: string
        missing:
          type: string
      required: [summary]
      additionalProperties: false
  - id: consume
    depends_on: [produce]
    bash: printf '%s|%s|%s' '${nodes.produce.output.summary}' '${nodes.produce.output.missing?}' '${nodes.produce.output.empty:-fallback}'
YAML
"$TAKT" validate "$WORK/render.yaml" --config "$WORK/.takt/config.yaml" --workspace "$WORK" --json >/dev/null
"$TAKT" run "$WORK/render.yaml" --config "$WORK/.takt/config.yaml" --workspace "$WORK" --json >"$WORK/render.json"
python3 - "$WORK/render.json" <<'PY'
import json, sys
value=json.load(open(sys.argv[1]))["result"]
assert value["status"] == "completed", value
assert value["nodes"]["consume"]["output"] == "ok||fallback", value
PY

cat > "$WORK/warning.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: warning
nodes:
  - id: work
    script:
      runtime: python
      inline: |
        print('{"summary":"ok"}')
    output_format:
      type: object
      properties:
        summary:
          type: string
YAML
"$TAKT" validate "$WORK/warning.yaml" --config "$WORK/.takt/config.yaml" --workspace "$WORK" --json >"$WORK/warning.json"
python3 - "$WORK/warning.json" <<'PY'
import json, sys
value=json.load(open(sys.argv[1]))["result"]
assert any(d["code"] == "schema.open_object" and d["severity"] == "warning" for d in value["diagnostics"]), value
PY
if "$TAKT" validate "$WORK/warning.yaml" --config "$WORK/.takt/config.yaml" --workspace "$WORK" --warnings-as-errors --json >"$WORK/warning-strict.out" 2>&1; then
  echo 'expected warnings-as-errors to fail' >&2
  exit 1
fi

cat > "$WORK/always.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: always-run
nodes:
  - id: fail
    bash: exit 9
  - id: cleanup
    depends_on: [fail]
    always_run: true
    bash: printf cleaned > cleanup.txt
YAML
if "$TAKT" run "$WORK/always.yaml" --config "$WORK/.takt/config.yaml" --workspace "$WORK" --json >"$WORK/always.out" 2>&1; then
  echo 'expected main Run to fail' >&2
  exit 1
fi
test "$(cat "$WORK/cleanup.txt")" = cleaned

echo 'authoring contract: PASS'
