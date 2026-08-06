#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cat >"$TMP/config.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
YAML
cat >"$TMP/workflow.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: mcp-contract
nodes:
  - id: complete
    bash: printf 'mcp-ok'
YAML

python3 - "$TMP/workflow.yaml" "$TMP/config.yaml" >"$TMP/requests.jsonl" <<'PY'
import json, sys
workflow, config = sys.argv[1:]
messages = [
    {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"contract","version":"1"}}},
    {"jsonrpc":"2.0","id":2,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"contract","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}},
    {"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}},
    {"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"takt.run.start","arguments":{"selector":workflow,"config_path":config,"detached":False}}},
]
for message in messages:
    print(json.dumps(message, separators=(",", ":")))
PY

"$ROOT/bin/takt" mcp --workspace "$TMP" --config "$TMP/config.yaml" \
  <"$TMP/requests.jsonl" >"$TMP/responses.jsonl"

python3 - "$TMP/responses.jsonl" <<'PY'
import json, sys
responses = {}
with open(sys.argv[1], encoding="utf-8") as fh:
    for line in fh:
        value = json.loads(line)
        responses[value.get("id")] = value
assert responses[1]["result"]["protocolVersion"] == "2025-11-25"
assert responses[2]["result"]["protocolVersion"] == "2026-07-28"
tools = responses[3]["result"]["tools"]
assert [tool["name"] for tool in tools] == [
    "takt.workflow.list", "takt.workflow.describe", "takt.run.start", "takt.run.get",
    "takt.run.resume", "takt.run.answer", "takt.run.cancel", "takt.run.children",
    "takt.run.artifacts", "takt.run.events", "takt.node.pending", "takt.node.claim",
    "takt.node.event", "takt.node.tool.request", "takt.node.tool.decide",
    "takt.node.tool.start", "takt.node.tool.complete", "takt.node.tool.get",
    "takt.node.tool.cancel", "takt.node.artifact.declare", "takt.node.complete",
    "takt.node.fail",
]
call = responses[4]["result"]
assert call["isError"] is False, call
assert call["structuredContent"]["state"]["status"] == "completed", call
assert call["structuredContent"]["state"]["nodes"]["complete"]["output"] == "mcp-ok", call
PY

echo 'local MCP contract: PASS'
