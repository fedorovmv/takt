#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cat > "$TMP/config.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
YAML

cat > "$TMP/child.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: governed-child
nodes:
  - id: approve
    approval:
      message: Approve the governed child
      capture_response: true
  - id: done
    depends_on: [approve]
    bash: printf 'child:%s' '${nodes.approve.output}'
YAML

cat > "$TMP/parent.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: governed-parent
nodes:
  - id: child
    workflow:
      path: child.yaml
      output_node: done
      isolation: inherit
  - id: finish
    depends_on: [child]
    bash: printf 'root:%s' '${nodes.child.output}'
YAML

"$ROOT/bin/takt" validate "$TMP/parent.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json >/dev/null
run_json="$("$ROOT/bin/takt" run "$TMP/parent.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json)"
root_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["id"])' <<<"$run_json")"
child_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["waiting"]["child_run_id"])' <<<"$run_json")"
python3 -c '
import json,sys
state=json.load(sys.stdin)["result"]
assert state["id"] == sys.argv[1]
assert state["status"] == "waiting"
assert state["waiting"]["kind"] == "child_run"
assert state["waiting"]["child_run_id"] == sys.argv[2]
assert state["child_run_ids"] == [sys.argv[2]]
' "$root_id" "$child_id" <<<"$run_json"

children_json="$("$ROOT/bin/takt" children "$root_id" --workspace "$TMP" --json)"
python3 -c '
import json,sys
value=json.load(sys.stdin)["result"]
assert value["run_id"] == sys.argv[1]
assert len(value["children"]) == 1
child=value["children"][0]
assert child["id"] == sys.argv[2]
assert child["status"] == "waiting"
assert child["parent_node_id"] == "child"
' "$root_id" "$child_id" <<<"$children_json"

answer_json="$("$ROOT/bin/takt" answer "$root_id" child --value yes --workspace "$TMP" --json)"
python3 -c '
import json,sys
state=json.load(sys.stdin)["result"]
assert state["id"] == sys.argv[1]
assert state["status"] == "completed"
assert state["output"] == "root:child:yes"
assert state["nodes"]["child"]["child_run_id"] == sys.argv[2]
' "$root_id" "$child_id" <<<"$answer_json"

child_status="$("$ROOT/bin/takt" status "$child_id" --workspace "$TMP" --json)"
python3 -c '
import json,sys
state=json.load(sys.stdin)["result"]
assert state["status"] == "completed"
assert state["parent_run_id"] == sys.argv[1]
assert state["parent_node_id"] == "child"
assert state["output"] == "child:yes"
' "$root_id" <<<"$child_status"

cancel_run_json="$("$ROOT/bin/takt" run "$TMP/parent.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json)"
cancel_root="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["id"])' <<<"$cancel_run_json")"
cancel_child="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["waiting"]["child_run_id"])' <<<"$cancel_run_json")"
"$ROOT/bin/takt" cancel "$cancel_root" --reason contract-cancel --workspace "$TMP" --json >/dev/null
for id in "$cancel_root" "$cancel_child"; do
  status_json="$("$ROOT/bin/takt" status "$id" --workspace "$TMP" --json)"
  python3 -c '
import json,sys
state=json.load(sys.stdin)["result"]
assert state["status"] == "cancelled"
assert state["cancel_requested"] is True
assert state["error_code"] == "cancelled"
' <<<"$status_json"
done

echo 'governed child run contract: PASS'
