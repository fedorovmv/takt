#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
TAKT="$ROOT/bin/takt"
WORK=$(mktemp -d)
cleanup() {
  "$TAKT" daemon stop --workspace "$WORK" --json >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT
mkdir -p "$WORK/.takt"

cat > "$WORK/.takt/config.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
YAML
cat > "$WORK/workflow.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: daemon-contract
nodes:
  - id: background
    bash: |
      sleep 1.5
      printf daemon-completed
YAML

"$TAKT" daemon start --workspace "$WORK" --json >"$WORK/daemon-start.json"
"$TAKT" daemon status --workspace "$WORK" --json >"$WORK/daemon-status-1.json"
"$TAKT" daemon status --workspace "$WORK" --json >"$WORK/daemon-status-2.json"
python3 - "$WORK/daemon-start.json" "$WORK/daemon-status-1.json" "$WORK/daemon-status-2.json" <<'PY'
import json, sys
values=[json.load(open(p))["result"] for p in sys.argv[1:]]
pid=values[0]["daemon"]["pid"]
assert values[0]["started"] is True
assert values[1]["running"] is True and values[2]["running"] is True
assert values[1]["daemon"]["pid"] == pid == values[2]["daemon"]["pid"]
PY

python3 - "$TAKT" "$WORK" <<'PY' >"$WORK/run.json"
import subprocess, sys, time
binary, workspace=sys.argv[1:]
started=time.monotonic()
result=subprocess.run([
    binary, "run", workspace+"/workflow.yaml", "--config", workspace+"/.takt/config.yaml",
    "--workspace", workspace, "--daemon", "--json"
], check=True, text=True, stdout=subprocess.PIPE)
elapsed=time.monotonic()-started
assert elapsed < 1.0, elapsed
print(result.stdout, end="")
PY
RUN_ID=$(python3 - "$WORK/run.json" <<'PY'
import json, sys
print(json.load(open(sys.argv[1]))["result"]["run_id"])
PY
)
test -n "$RUN_ID"

"$TAKT" events "$RUN_ID" --workspace "$WORK" --daemon --follow --json >"$WORK/events.jsonl"
python3 - "$WORK/events.jsonl" <<'PY'
import json, sys
values=[json.loads(line) for line in open(sys.argv[1]) if line.strip()]
assert values
assert any(v["type"] == "run.completed" for v in values), values[-5:]
PY
"$TAKT" status "$RUN_ID" --workspace "$WORK" --json >"$WORK/status.json"
python3 - "$WORK/status.json" <<'PY'
import json, sys
value=json.load(open(sys.argv[1]))["result"]
assert value["status"] == "completed", value
assert value["output"] == "daemon-completed", value
PY

printf '%s\n' '{"jsonrpc":"2.0","id":"daemon-tools","method":"tools/list","params":{}}' \
  | "$TAKT" mcp --daemon --workspace "$WORK" >"$WORK/mcp.json"
python3 - "$WORK/mcp.json" <<'PY'
import json, sys
value=json.load(open(sys.argv[1]))
assert len(value["result"]["tools"]) == 29, value
PY

"$TAKT" daemon stop --workspace "$WORK" --json >"$WORK/daemon-stop.json"
if "$TAKT" daemon status --workspace "$WORK" --json >/dev/null 2>&1; then
  echo 'daemon still reports running after stop' >&2
  exit 1
fi

echo 'daemon contract: PASS'
