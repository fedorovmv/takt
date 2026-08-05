#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/skills/review"
printf '# Review skill\n' > "$TMP/skills/review/SKILL.md"
printf '{"mcp":{"search":{"type":"remote","url":"https://example.invalid/mcp"}}}\n' > "$TMP/mcp.json"

cat > "$TMP/config.yaml" <<YAML
apiVersion: takt/v1alpha1
kind: Config
models:
  test:
    provider: fake
    id: fake
assistants:
  fake:
    type: process
    argv: ["$ROOT/bin/takt-fake-assistant", --case, success]
    capabilities: [tool_policy, skills, mcp, sandbox_filesystem, custom]
    protocol: takt-assistant/v1alpha1
YAML

cat > "$TMP/workflow.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: policy-contract
defaults:
  assistant: fake
  model: test
nodes:
  - id: classify
    prompt: classify without tools
    allowed_tools: []
    skills: []
    denied_tools: [write]
    mcp: mcp.json
    sandbox:
      filesystem: read_only
    requires: [custom]
YAML

$ROOT/bin/takt run "$TMP/workflow.yaml" --config "$TMP/config.yaml" --workspace "$TMP" --json > "$TMP/result.json"
python3 - "$TMP" "$TMP/result.json" <<'PY'
import json, os, sys
root=sys.argv[1]
with open(sys.argv[2]) as f:
    state=json.load(f)["result"]
policy=state["nodes"]["classify"]["policy"]
assert policy["tools_restricted"] is True
assert policy.get("allowed_tools", []) == []
assert policy["skills_restricted"] is True
assert policy.get("skills", []) == []
assert policy["denied_tools"] == ["write"]
assert policy["filesystem"] == "read_only"
assert policy["mcp_path"] == os.path.join(root, "mcp.json")
assert policy["requires"] == ["custom"]
assert policy["capabilities"] == ["custom", "mcp", "sandbox_filesystem", "skills", "tool_policy"]
PY

cat > "$TMP/unsupported.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
models:
  test:
    provider: fake
    id: fake
assistants:
  fake:
    type: process
    argv: ["/usr/bin/false"]
    capabilities: [tool_policy]
    protocol: takt-assistant/v1alpha1
YAML

if "$ROOT/bin/takt" run "$TMP/workflow.yaml" --config "$TMP/unsupported.yaml" --workspace "$TMP" --json >"$TMP/unsupported.out" 2>"$TMP/unsupported.err"; then
  echo 'unsupported policy unexpectedly ran' >&2
  exit 1
fi
grep -q 'required capabilities' "$TMP/unsupported.err" "$TMP/unsupported.out"

echo 'node capability policy contract: PASS'
