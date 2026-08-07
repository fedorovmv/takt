#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
cleanup() {
  "$ROOT/bin/takt" daemon stop --workspace "$TMP/project" --json >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT

mkdir -p "$ROOT/bin"
go build -o "$ROOT/bin/takt" "$ROOT/cmd/takt"
go build -o "$ROOT/bin/takt-fake-code-agent" "$ROOT/cmd/takt-fake-code-agent"
"$ROOT/bin/takt" init code --dir "$TMP/project" --json >/dev/null

cat > "$TMP/project/.takt/config.yaml" <<CFG
apiVersion: takt/v1alpha1
kind: Config
default_assistant: opencode
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
  opencode:
    type: process
    argv:
      - $ROOT/bin/takt-fake-code-agent
    capabilities:
      - tool_policy
      - skills
      - sandbox_filesystem
CFG

for file in \
  "$TMP/project/.takt/profiles/code/workflows/dynamic-plan.yaml" \
  "$TMP/project/.takt/profiles/code/workflows/dynamic-replan.yaml" \
  "$TMP/project/.takt/profiles/code/workflows/blocks/dynamic-discover.yaml" \
  "$TMP/project/.takt/profiles/code/workflows/blocks/dynamic-investigate.yaml" \
  "$TMP/project/.takt/profiles/code/workflows/blocks/dynamic-implement.yaml" \
  "$TMP/project/.takt/profiles/code/workflows/blocks/dynamic-validate.yaml" \
  "$TMP/project/.takt/profiles/code/workflows/blocks/dynamic-review.yaml" \
  "$TMP/project/.takt/profiles/code/workflows/blocks/dynamic-adversarial-verify.yaml" \
  "$TMP/project/.takt/profiles/code/workflows/blocks/dynamic-synthesize.yaml" \
  "$TMP/project/.takt/profiles/code/workflows/blocks/dynamic-repository-change.yaml" \
  "$TMP/project/.takt/profiles/code/workflows/blocks/dynamic-integration-verify.yaml"
do
  "$ROOT/bin/takt" validate "$file" --workspace "$TMP/project" --config "$TMP/project/.takt/config.yaml" --json >/dev/null
done

go test "$ROOT/internal/dynamicplan" -count=1 >/dev/null
go test "$ROOT/internal/control" -run 'TestPlanCandidateProducesPreviewAndRequiresConfirmation|TestPromoteCompletedPlanCreatesProjectWorkflow' -count=1 >/dev/null

grep -q 'case "plan"' "$ROOT/cmd/takt/main.go"
grep -q 'case "execute"' "$ROOT/cmd/takt/main.go"
grep -q 'case "steer"' "$ROOT/cmd/takt/main.go"
grep -q 'takt.plan.promote' "$ROOT/internal/mcp/server.go"
grep -q 'Dynamic Takt из кодинг-агента' "$ROOT/skills/takt/SKILL.md"

"$ROOT/bin/takt" block validate "$TMP/project/.takt/profiles/code/workflows/blocks/package.yaml" --json >/dev/null
"$ROOT/bin/takt" block list --workspace "$TMP/project" --profile code --json > "$TMP/blocks.json"
python3 - "$TMP/blocks.json" <<'PY'
import json,sys
value=json.load(open(sys.argv[1]))['result']
assert len(value['blocks'])==11, value
assert any(b['name']=='adversarial-verify' and b['workflow_path'].endswith('dynamic-adversarial-verify.yaml') for b in value['blocks']), value
assert value['fingerprint'], value
PY

# The documented CLI path must work without a daemon: foreground execution
# advances all segments before the CLI process exits.
"$ROOT/bin/takt" plan 'fixture dynamic audit' --workspace "$TMP/project" --json > "$TMP/direct-plan.json"
DIRECT_PLAN_ID="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["result"]["plan_id"])' "$TMP/direct-plan.json")"
"$ROOT/bin/takt" execute "$DIRECT_PLAN_ID" --confirm --workspace "$TMP/project" --json > "$TMP/direct-execute.json"
python3 - "$TMP/direct-execute.json" <<'PY'
import json,sys
value=json.load(open(sys.argv[1]))['result']
assert value['status']=='completed', value
assert value['completed_phases']==['inventory','summary'], value
PY

"$ROOT/bin/takt" daemon start --workspace "$TMP/project" --json >/dev/null
"$ROOT/bin/takt" plan 'fixture dynamic audit' --workspace "$TMP/project" --json > "$TMP/plan.json"
PLAN_ID="$(python3 - "$TMP/plan.json" <<'PY'
import json,sys
value=json.load(open(sys.argv[1]))['result']
assert value['decision']=='planned', value
assert value['requires_confirmation'] is True, value
assert 'inventory' in value['preview'] and 'Budget:' in value['preview'], value
print(value['plan_id'])
PY
)"

printf '%s\n' "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"takt.execute\",\"arguments\":{\"plan_id\":\"$PLAN_ID\",\"confirm\":true}}}" \
  | "$ROOT/bin/takt" mcp --daemon --surface all --workspace "$TMP/project" > "$TMP/execute.json"
python3 - "$TMP/execute.json" <<'PY'
import json,sys
value=json.load(open(sys.argv[1]))
assert value['result']['isError'] is False, value
assert value['result']['structuredContent']['status'] in ('running','completed'), value
PY

status=""
for _ in $(seq 1 30); do
  "$ROOT/bin/takt" plan get "$PLAN_ID" --workspace "$TMP/project" --json > "$TMP/plan-get.json"
  status="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["result"]["record"]["status"])' "$TMP/plan-get.json")"
  case "$status" in
    completed|failed|cancelled|waiting) break ;;
  esac
  sleep 1
done
[ "$status" = completed ] || {
  cat "$TMP/plan-get.json" >&2
  echo "dynamic plan ended with status $status" >&2
  exit 1
}
python3 - "$TMP/plan-get.json" <<'PY'
import json,sys
value=json.load(open(sys.argv[1]))['result']
record=value['record']
assert record['completed_phases']==['inventory','summary'], record
assert len(record['execution_run_ids'])==2, record
assert all(phase['status']=='completed' for phase in value['phases']), value
assert value['artifact_count']==2, value
assert all(run['status']=='completed' for run in value['runs']), value
PY

"$ROOT/bin/takt" plan promote "$PLAN_ID" --name fixture-dynamic --workspace "$TMP/project" --json > "$TMP/promote.json"
GENERATED="$TMP/project/.takt/workflows/generated/fixture-dynamic.yaml"
test -f "$GENERATED"
"$ROOT/bin/takt" validate "$GENERATED" --workspace "$TMP/project" --config "$TMP/project/.takt/config.yaml" --json >/dev/null
grep -q '\${input}' "$GENERATED"

printf '%s\n' 'dynamic Takt contract: PASS'
