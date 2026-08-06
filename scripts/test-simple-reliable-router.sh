#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$ROOT/bin"
go build -o "$ROOT/bin/takt" "$ROOT/cmd/takt"
go build -o "$ROOT/bin/takt-fake-code-agent" "$ROOT/cmd/takt-fake-code-agent"
"$ROOT/bin/takt" init code --dir "$TMP/project" --json >/dev/null
git -C "$TMP/project" init -q
git -C "$TMP/project" config user.email fixture@example.com
git -C "$TMP/project" config user.name Fixture
printf 'fixture\n' > "$TMP/project/README.fixture"
git -C "$TMP/project" add README.fixture .takt
git -C "$TMP/project" commit -qm 'fixture baseline'

cat > "$TMP/project/.takt/config.yaml" <<CFG
apiVersion: takt/v1alpha1
kind: Config
default_assistant: fixture
models:
  routing:
    provider: fixture
    id: routing
  implementation:
    provider: fixture
    id: implementation
  review:
    provider: fixture
    id: review
assistants:
  fixture:
    type: process
    argv: [$ROOT/bin/takt-fake-code-agent]
    env:
      FAKE_DYNAMIC_VALIDATE_FAIL_ONCE_FILE: $TMP/project/.takt/plans/.dynamic-validation-fail-once
    capabilities: [tool_policy, skills, mcp, sandbox_filesystem]
CFG
git -C "$TMP/project" add .takt/config.yaml
git -C "$TMP/project" commit -qm 'configure fixture adapter'
cat >> "$TMP/project/.git/info/exclude" <<'EXCLUDE'
.takt/plans/
.takt/runs/
.takt/host-sessions/
.takt/notifications/
.takt/locks/
EXCLUDE

"$ROOT/bin/takt" validate "$TMP/project/.takt/profiles/code/workflows/task-route.yaml" \
  --workspace "$TMP/project" --config "$TMP/project/.takt/config.yaml" --json >/dev/null

"$ROOT/bin/takt" task start 'Implement an ordinary repository change' \
  --workspace "$TMP/project" --json > "$TMP/preview.json"
PLAN_ID="$(python3 - "$TMP/preview.json" <<'PY'
import json,sys
value=json.load(open(sys.argv[1]))['result']
assert value['kind']=='plan', value
assert value['status']=='draft', value
assert value['needs_input'] is True, value
assert value['route']['route']=='template', value
assert value['route']['template']=='simple-reliable', value
assert [p['uses'] for p in value.get('plan',{}).get('phases',[])] in ([],), value
print(value['plan_id'])
PY
)"

"$ROOT/bin/takt" task explain "$PLAN_ID" --workspace "$TMP/project" --json > "$TMP/explain.json"
python3 - "$TMP/explain.json" <<'PY'
import json,sys
value=json.load(open(sys.argv[1]))['result']
assert value['route']['route']=='template', value
assert [p['uses'] for p in value['plan']['phases']]==['investigate','implement','validate','review'], value
PY

"$ROOT/bin/takt" task start 'Change the public API safely' --go \
  --workspace "$TMP/project" --json > "$TMP/protected.json"
python3 - "$TMP/protected.json" <<'PY'
import json,sys
value=json.load(open(sys.argv[1]))['result']
assert value['route']['route']=='template', value
controls=value['route']['controls']
assert controls['baseline'] and controls['independent_tests'] and controls['enhanced_review'], value
assert value['status']=='completed', value
PY

touch "$TMP/project/.takt/plans/.dynamic-validation-fail-once"
"$ROOT/bin/takt" task start 'Implement change with recoverable validation failure' --go \
  --workspace "$TMP/project" --json > "$TMP/repair.json"
REPAIR_PLAN_ID="$(python3 - "$TMP/repair.json" <<'PY'
import json,sys
value=json.load(open(sys.argv[1]))['result']
assert value['status']=='completed', value
print(value['plan_id'])
PY
)"
"$ROOT/bin/takt" task explain "$REPAIR_PLAN_ID" --workspace "$TMP/project" --json > "$TMP/repair-explain.json"
python3 - "$TMP/repair-explain.json" "$TMP/project" <<'PY'
import json,sys,pathlib
value=json.load(open(sys.argv[1]))['result']
record=value['plan']['record']
assert record['repair_attempts'].get('validate:deterministic-validation') == 1, record
checks=record['check_results']
validation=[c for c in checks if c['name']=='deterministic-validation']
assert [c['passed'] for c in validation] == [False, True], validation
assert value['status']=='completed', value
runs=record['execution_run_ids']
assert len(runs) >= 2, runs
workspace=record.get('execution_workspace')
assert workspace, record
states=[json.load(open(pathlib.Path(sys.argv[2])/'.takt'/'runs'/rid/'state.json')) for rid in runs]
assert states[0].get('worktree',{}).get('enabled') is True, states[0]
assert states[-1].get('execution_workspace') == workspace, (workspace, states[-1])
assert states[-1].get('worktree') in (None, {}), states[-1]
PY

printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  | "$ROOT/bin/takt" mcp --workspace "$TMP/project" > "$TMP/agent-tools.json"
python3 - "$TMP/agent-tools.json" <<'PY'
import json,sys
value=json.loads(open(sys.argv[1]).readline())
assert [t['name'] for t in value['result']['tools']] == [
  'takt.task.start','takt.task.status','takt.task.respond','takt.task.stop','takt.task.explain'
], value
PY

go test "$ROOT/internal/taskroute" "$ROOT/internal/control" -run 'TestCompileSimpleReliable|TestPlanFallsBackToStableTemplate' -count=1 >/dev/null

grep -q 'default_assistant' "$ROOT/schemas/config.schema.json"
grep -q 'takt-assistant/v1alpha2' "$ROOT/schemas/config.schema.json"
grep -q 'task-route.schema.json' "$ROOT/schemas/task-route.schema.json"
grep -q 'assistant: coding-agent' "$TMP/project/.takt/profiles/code/workflows/task-route.yaml"

echo 'simple reliable task router contract: PASS'
