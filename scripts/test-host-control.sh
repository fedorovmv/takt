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
CFG
"$ROOT/bin/takt" daemon start --workspace "$TMP/project" --json >/dev/null

# Strict cannot be claimed by a soft skill/MCP integration.
if "$ROOT/bin/takt" host begin 'fixture dynamic audit' --host pi --host-session incomplete \
  --enforcement strict --tool-call-blocking --workspace "$TMP/project" --daemon --json >"$TMP/incomplete.json" 2>/dev/null; then
  echo 'incomplete strict capabilities unexpectedly accepted' >&2; exit 1
fi

"$ROOT/bin/takt" host begin 'fixture dynamic audit' --host pi --host-session pi-session-1 \
  --enforcement strict --command-interception --input-interception --tool-call-blocking \
  --completion-blocking --session-recovery --workspace "$TMP/project" --daemon --json > "$TMP/begin.json"
SESSION_ID="$(python3 - "$TMP/begin.json" <<'PY'
import json,sys
v=json.load(open(sys.argv[1]))
assert v['ok'] is True, v
r=v['result']
assert r['session']['enforcement']=='strict', r
assert r['session']['status']=='preview', r
assert 'Budget:' in r['plan']['preview'], r
print(r['session']['id'])
PY
)"

# A daemon restart must not lose the durable managed-session binding.
"$ROOT/bin/takt" daemon stop --workspace "$TMP/project" --json >/dev/null
"$ROOT/bin/takt" daemon start --workspace "$TMP/project" --json >/dev/null
"$ROOT/bin/takt" host find --host pi --host-session pi-session-1 --workspace "$TMP/project" --daemon --json > "$TMP/find.json"
python3 - "$TMP/find.json" "$SESSION_ID" <<'PY'
import json,sys
r=json.load(open(sys.argv[1]))['result']
assert r['session']['id']==sys.argv[2], r
PY

"$ROOT/bin/takt" host guard-tool "$SESSION_ID" --tool edit --read-only --workspace "$TMP/project" --daemon --json > "$TMP/edit.json"
"$ROOT/bin/takt" host guard-tool "$SESSION_ID" --tool grep --workspace "$TMP/project" --daemon --json > "$TMP/grep.json"
"$ROOT/bin/takt" host guard-completion "$SESSION_ID" --kind final --workspace "$TMP/project" --daemon --json > "$TMP/final.json"
python3 - "$TMP/edit.json" "$TMP/grep.json" "$TMP/final.json" <<'PY'
import json,sys
edit=json.load(open(sys.argv[1]))['result']; grep=json.load(open(sys.argv[2]))['result']; final=json.load(open(sys.argv[3]))['result']
assert edit['allowed'] is False, edit
assert grep['allowed'] is True, grep
assert final['allowed'] is False, final
PY

"$ROOT/bin/takt" host confirm "$SESSION_ID" --confirm --workspace "$TMP/project" --daemon --json >/dev/null
status=''
for _ in $(seq 1 30); do
  "$ROOT/bin/takt" host status "$SESSION_ID" --workspace "$TMP/project" --daemon --json > "$TMP/status.json"
  status="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["result"]["session"]["status"])' "$TMP/status.json")"
  case "$status" in completed|failed|released) break;; esac
  sleep 1
done
[ "$status" = completed ] || { cat "$TMP/status.json" >&2; exit 1; }
"$ROOT/bin/takt" host guard-completion "$SESSION_ID" --kind final --workspace "$TMP/project" --daemon --json > "$TMP/done.json"
python3 - "$TMP/done.json" <<'PY'
import json,sys
assert json.load(open(sys.argv[1]))['result']['allowed'] is True
PY

# The same host session can start a new task after the previous plan completed.
"$ROOT/bin/takt" host begin 'fixture dynamic audit again' --host pi --host-session pi-session-1 \
  --enforcement guarded --command-interception --input-interception --tool-call-blocking \
  --session-recovery --workspace "$TMP/project" --daemon --json > "$TMP/begin-again.json"
python3 - "$TMP/begin-again.json" "$SESSION_ID" <<'PY'
import json,sys
r=json.load(open(sys.argv[1]))['result']['session']
assert r['status']=='preview', r
assert r['id'] != sys.argv[2], r
PY

# Static contract for both native extensions: CLI envelope is unwrapped and
# interception/gating hooks are present.
grep -q 'envelope.result' "$ROOT/integrations/coding-agent-host-control/pi/index.ts"
grep -q 'pi.registerCommand("takt"' "$ROOT/integrations/coding-agent-host-control/pi/index.ts"
grep -q 'pi.on("input"' "$ROOT/integrations/coding-agent-host-control/pi/index.ts"
grep -q 'pi.on("tool_call"' "$ROOT/integrations/coding-agent-host-control/pi/index.ts"
grep -q 'ctx.session.hook("context"' "$ROOT/integrations/coding-agent-host-control/opencode/index.ts"
grep -q 'ctx.tool.hook("execute.before"' "$ROOT/integrations/coding-agent-host-control/opencode/index.ts"
grep -q 'The main LLM was not invoked' "$ROOT/integrations/coding-agent-host-control/opencode/index.ts"

grep -q 'return { action: "handled" as const }' "$ROOT/integrations/coding-agent-host-control/pi/index.ts"
! grep -q 'before_agent_start' "$ROOT/integrations/coding-agent-host-control/pi/index.ts"
grep -q 'ctx.shell.hook("create.before"' "$ROOT/integrations/coding-agent-host-control/opencode/index.ts"
grep -q '\["host", "status", cached.id\]' "$ROOT/integrations/coding-agent-host-control/pi/index.ts"
grep -q '\["host", "status", cached.id\]' "$ROOT/integrations/coding-agent-host-control/opencode/index.ts"
grep -q '"--enforcement", "guarded"' "$ROOT/integrations/coding-agent-host-control/pi/index.ts"
grep -q '"--enforcement", "guarded"' "$ROOT/integrations/coding-agent-host-control/opencode/index.ts"
! grep -q 'completion-blocking' "$ROOT/integrations/coding-agent-host-control/pi/index.ts"
grep -q '"verified": false' "$ROOT/integrations/coding-agent-host-control/opencode/package.json"
grep -q '"enforcement": "guarded"' "$ROOT/integrations/coding-agent-host-control/opencode/package.json"
! grep -Eq '"(next|\*)"' "$ROOT/integrations/coding-agent-host-control/opencode/package.json"

echo 'coding-agent host control contract: PASS'
