#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cat > "$TMP/config.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
YAML

cat > "$TMP/schema.json" <<'JSON'
{"kind":"contract"}
JSON

cat > "$TMP/emit.sh" <<'SH'
#!/bin/sh
set -eu
printf '# Script report\nrun=%s node=%s attempt=%s\n' "$TAKT_RUN_ID" "$TAKT_NODE_ID" "$TAKT_ATTEMPT" > report.md
printf '{"status":"ok","count":2}\n'
SH
chmod +x "$TMP/emit.sh"

cat > "$TMP/workflow.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: script-artifacts-contract
nodes:
  - id: execute
    script:
      runtime: command
      path: emit.sh
      dependencies: [schema.json]
    output_format:
      type: object
      additionalProperties: false
      properties:
        status:
          type: string
          enum: [ok]
        count:
          type: integer
      required: [status, count]
    output_type: report
    output_mime: text/markdown
    output_path: report.md
YAML

"$ROOT/bin/takt" validate "$TMP/workflow.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json >/dev/null
run_json="$("$ROOT/bin/takt" run "$TMP/workflow.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json)"
run_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["id"])' <<<"$run_json")"
python3 -c '
import json,sys
state=json.load(sys.stdin)["result"]
node=state["nodes"]["execute"]
assert state["status"] == "completed"
assert node["output"] == "{\"count\":2,\"status\":\"ok\"}"
assert "{\"status\":\"ok\",\"count\":2}" in node["stdout"]
assert len(node["artifacts"]) == 1
assert len(state["artifacts"]) == 1
' <<<"$run_json"

artifacts_json="$("$ROOT/bin/takt" artifacts "$run_id" --workspace "$TMP" --type report --json)"
artifact_path="$(python3 -c '
import json,os,sys
value=json.load(sys.stdin)["result"]
assert value["run_id"] == sys.argv[1]
assert len(value["artifacts"]) == 1
artifact=value["artifacts"][0]
assert artifact["type"] == "report"
assert artifact["mime"] == "text/markdown"
assert artifact["producer_node_id"] == "execute"
assert len(artifact["sha256"]) == 64
assert artifact["size"] > 0
assert os.path.isfile(artifact["path"])
print(artifact["path"])
' "$run_id" <<<"$artifacts_json")"
grep -q '^# Script report$' "$artifact_path"
grep -q 'node=execute attempt=1' "$artifact_path"

# A dependency change must invalidate resume fingerprints.
cat > "$TMP/wait.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: script-fingerprint-contract
nodes:
  - id: execute
    script:
      runtime: command
      path: emit.sh
      dependencies: [schema.json]
  - id: approve
    depends_on: [execute]
    approval:
      message: continue
YAML
wait_json="$("$ROOT/bin/takt" run "$TMP/wait.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json)"
wait_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["id"])' <<<"$wait_json")"
printf '{"kind":"changed"}\n' > "$TMP/schema.json"
if "$ROOT/bin/takt" answer "$wait_id" approve --value yes --workspace "$TMP" --json >/dev/null 2>"$TMP/fingerprint.err"; then
  echo "script dependency change did not block resume" >&2
  exit 1
fi
grep -q 'workflow definition changed' "$TMP/fingerprint.err"

echo 'script and typed artifact contract: PASS'
