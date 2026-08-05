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
  name: fanout-child
nodes:
  - id: approve
    approval:
      message: Approve ${input}
      capture_response: true
  - id: done
    depends_on: [approve]
    bash: printf '%s:%s' '${input}' '${nodes.approve.output}'
YAML

cat > "$TMP/parent.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: fanout-parent
nodes:
  - id: discover
    bash: printf '[{"name":"first"},{"name":"second"}]'
  - id: children
    depends_on: [discover]
    workflow:
      path: child.yaml
      input: '${item.name}'
      output_node: done
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output
        as: item
        max_parallel: 2
        join: all_done
YAML

"$ROOT/bin/takt" validate "$TMP/parent.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json >/dev/null
run_json="$("$ROOT/bin/takt" run "$TMP/parent.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json)"
root_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["result"]["id"])' <<<"$run_json")"
mapfile_tmp="$TMP/child-ids"
python3 -c '
import json,sys
state=json.load(sys.stdin)["result"]
assert state["status"] == "waiting"
assert state["waiting"]["kind"] == "child_run"
assert len(state["waiting"]["child_run_ids"]) == 2
for value in state["waiting"]["child_run_ids"]:
    print(value)
' <<<"$run_json" > "$mapfile_tmp"
first_child="$(sed -n '1p' "$mapfile_tmp")"
second_child="$(sed -n '2p' "$mapfile_tmp")"

children_json="$("$ROOT/bin/takt" children "$root_id" --workspace "$TMP" --json)"
python3 -c '
import json,sys
value=json.load(sys.stdin)["result"]
children=value["children"]
assert len(children) == 2
assert children[0]["fan_out"]["index"] == 0
assert children[0]["fan_out"]["item"]["name"] == "first"
assert children[1]["fan_out"]["index"] == 1
assert children[1]["fan_out"]["item"]["name"] == "second"
' <<<"$children_json"

if "$ROOT/bin/takt" answer "$root_id" children --value ambiguous --workspace "$TMP" --json >/dev/null 2>&1; then
  echo 'root answer unexpectedly selected one of multiple waiting children' >&2
  exit 1
fi

partial_json="$("$ROOT/bin/takt" answer "$first_child" approve --value yes --workspace "$TMP" --json)"
python3 -c '
import json,sys
state=json.load(sys.stdin)["result"]
assert state["id"] == sys.argv[1]
assert state["status"] == "waiting"
assert state["waiting"]["child_run_ids"] == [sys.argv[2]]
' "$root_id" "$second_child" <<<"$partial_json"

"$ROOT/bin/takt" cancel "$second_child" --reason selective-cancel --workspace "$TMP" --json >/dev/null
final_json="$("$ROOT/bin/takt" resume "$root_id" --workspace "$TMP" --json)"
python3 -c '
import json,sys
state=json.load(sys.stdin)["result"]
assert state["status"] == "completed"
records=json.loads(state["nodes"]["children"]["output"])
assert [r["status"] for r in records] == ["completed", "cancelled"]
assert records[0]["output"] == "first:yes"
assert records[1]["run_id"] == sys.argv[1]
' "$second_child" <<<"$final_json"

printf '%s\n' 'governed child fan-out contract: PASS'
