#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cat >"$TMP/config.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: contract
    id: external
assistants:
  delegated:
    type: mock
YAML
cat >"$TMP/workflow.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: external-executor-contract
defaults:
  assistant: delegated
  model: demo
nodes:
  - id: delegated
    prompt: Return {"ok":true} after inspecting the project.
    executor: external
    allowed_tools: []
    output_format:
      type: object
      properties:
        ok:
          type: boolean
      required: [ok]
    output_type: result
    output_mime: application/json
  - id: verify
    depends_on: [delegated]
    bash: test '${nodes.delegated.output.ok}' = true
YAML

python3 - "$ROOT/bin/takt" "$TMP" "$TMP/workflow.yaml" "$TMP/config.yaml" <<'PY'
import json, subprocess, sys
binary, workspace, workflow, config = sys.argv[1:]
p = subprocess.Popen([binary, "mcp", "--workspace", workspace, "--config", config], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
next_id = 1
def call(name, arguments):
    global next_id
    request = {"jsonrpc":"2.0","id":next_id,"method":"tools/call","params":{"name":name,"arguments":arguments}}
    next_id += 1
    p.stdin.write(json.dumps(request, separators=(",", ":")) + "\n"); p.stdin.flush()
    response = json.loads(p.stdout.readline())
    assert "error" not in response, response
    result = response["result"]
    assert not result["isError"], result
    return result["structuredContent"]
start = call("takt.run.start", {"selector":workflow,"config_path":config,"detached":False})
run_id = start["run_id"]
assert start["state"]["status"] == "waiting", start
pending = call("takt.node.pending", {"run_id":run_id})
assert [task["node_id"] for task in pending["tasks"]] == ["delegated"], pending
claim = call("takt.node.claim", {"run_id":run_id,"node_id":"delegated","worker_id":"contract-worker","capabilities":["tool_policy"],"lease_ms":60000})
token = claim["claim_token"]
assert token
call("takt.node.event", {"run_id":run_id,"node_id":"delegated","claim_token":token,"event":{"type":"tool.started","tool":"read","call_id":"1","input":{"path":"README.md"}}})
completed = call("takt.node.complete", {"run_id":run_id,"node_id":"delegated","claim_token":token,"structured":{"ok":True},"stdout":"raw-stream","usage":{"input_tokens":5,"output_tokens":2,"cost":0.001}})
assert completed["status"] == "completed", completed
assert completed["nodes"]["delegated"]["stdout"] == "raw-stream", completed
assert completed["nodes"]["delegated"]["artifacts"][0]["type"] == "result", completed
events = call("takt.run.events", {"run_id":run_id,"after_revision":0})
assert any(event["type"] == "assistant.tool.started" for event in events["events"]), events
p.stdin.close(); p.wait(timeout=5)
assert p.returncode == 0, p.stderr.read()
PY

echo 'external node executor contract: PASS'
