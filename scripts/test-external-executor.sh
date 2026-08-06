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
    allowed_tools: [read]
    requires: [tool_policy, tool_control, agent_events_v2, tool_events]
    tool_approval:
      mode: required
      tools: [read]
      message: "Allow ${tool}?"
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
import json, os, subprocess, sys
binary, workspace, workflow, config = sys.argv[1:]
p = subprocess.Popen([binary, "mcp", "--surface", "all", "--workspace", workspace, "--config", config], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
next_id = 1
def call(name, arguments, expect_error=False):
    global next_id
    request = {"jsonrpc":"2.0","id":next_id,"method":"tools/call","params":{"name":name,"arguments":arguments}}
    next_id += 1
    p.stdin.write(json.dumps(request, separators=(",", ":")) + "\n"); p.stdin.flush()
    response = json.loads(p.stdout.readline())
    assert "error" not in response, response
    result = response["result"]
    if expect_error:
        assert result["isError"], result
    else:
        assert not result["isError"], result
    return result.get("structuredContent", {})
start = call("takt.run.start", {"selector":workflow,"config_path":config,"detached":False})
run_id = start["run_id"]
assert start["state"]["status"] == "waiting", start
pending = call("takt.node.pending", {"run_id":run_id})
assert [task["node_id"] for task in pending["tasks"]] == ["delegated"], pending
claim = call("takt.node.claim", {
    "run_id":run_id,"node_id":"delegated","worker_id":"contract-worker","lease_ms":60000,
    "capability_declaration":{
        "protocol":"takt-agent-events/v2",
        "capabilities":["tool_policy","tool_control","agent_events_v2","tool_events"],
        "event_types":["session.started","tool.requested","tool.allowed","tool.started","tool.completed","artifact.declared","usage","completed"],
        "session_events":True,"tool_events":True,"tool_control":True,"artifact_events":True,"usage_events":True
    }
})
token = claim["claim_token"]
assert token
call("takt.node.event", {"run_id":run_id,"node_id":"delegated","claim_token":token,"event":{"type":"session.started","session_id":"session-1"}})
requested = call("takt.node.tool.request", {"run_id":run_id,"node_id":"delegated","claim_token":token,"call_id":"read-1","tool":"read","input":{"path":"README.md"},"wait_ms":0})
assert requested["status"] == "waiting_approval", requested
call("takt.node.complete", {"run_id":run_id,"node_id":"delegated","claim_token":token,"structured":{"ok":True}}, expect_error=True)
allowed = call("takt.node.tool.decide", {"run_id":run_id,"node_id":"delegated","call_id":"read-1","decision":"allow","reason":"contract approval"})
assert allowed["status"] == "allowed", allowed
running = call("takt.node.tool.start", {"run_id":run_id,"node_id":"delegated","claim_token":token,"call_id":"read-1"})
assert running["status"] == "running", running
artifact_source = os.path.join(claim["workspace"], "tool-evidence.txt")
with open(artifact_source, "w", encoding="utf-8") as fh:
    fh.write("tool evidence\n")
artifact = call("takt.node.artifact.declare", {"run_id":run_id,"node_id":"delegated","claim_token":token,"call_id":"read-1","type":"tool-evidence","mime":"text/plain","path":artifact_source})
assert artifact["call_id"] == "read-1", artifact
finished_tool = call("takt.node.tool.complete", {"run_id":run_id,"node_id":"delegated","claim_token":token,"call_id":"read-1","output":{"ok":True}})
assert finished_tool["status"] == "completed", finished_tool
completed = call("takt.node.complete", {"run_id":run_id,"node_id":"delegated","claim_token":token,"structured":{"ok":True},"stdout":"raw-stream","session_id":"session-1","usage":{"input_tokens":5,"output_tokens":2,"cost":0.001}})
assert completed["status"] == "completed", completed
assert completed["nodes"]["delegated"]["stdout"] == "raw-stream", completed
artifact_types = {item["type"] for item in completed["nodes"]["delegated"]["artifacts"]}
assert {"tool-evidence", "result"}.issubset(artifact_types), completed
events = call("takt.run.events", {"run_id":run_id,"after_revision":0})
types = {event["type"] for event in events["events"]}
for expected in ["assistant.session.started", "assistant.tool.requested", "assistant.tool.allowed", "assistant.tool.started", "assistant.artifact.declared", "assistant.tool.completed", "assistant.usage", "assistant.completed"]:
    assert expected in types, (expected, types)
p.stdin.close(); p.wait(timeout=5)
assert p.returncode == 0, p.stderr.read()
PY
echo 'external node executor contract: PASS'
