#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
cleanup() {
  "$ROOT/bin/takt" daemon stop --workspace "$TMP/project" --json >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT

mkdir -p "$ROOT/bin" "$TMP/project/.takt"
go build -o "$ROOT/bin/takt" "$ROOT/cmd/takt"
cat > "$TMP/project/.takt/config.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
YAML
cat > "$TMP/project/pause.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: autonomous-pause
nodes:
  - id: first
    bash: sleep 2
  - id: second
    depends_on: [first]
    bash: printf done > autonomous-result.txt
YAML
cat > "$TMP/project/retry.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: autonomous-retry
nodes:
  - id: validate
    bash: test -f retry-gate
YAML
cat > "$TMP/project/abandon.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: autonomous-abandon
nodes:
  - id: long
    bash: sleep 30
YAML

"$ROOT/bin/takt" daemon start --workspace "$TMP/project" --json >/dev/null
"$ROOT/bin/takt" run "$TMP/project/pause.yaml" --config "$TMP/project/.takt/config.yaml" --workspace "$TMP/project" --daemon --json > "$TMP/start.json"
RUN_ID="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["result"]["run_id"])' "$TMP/start.json")"
sleep 0.08
"$ROOT/bin/takt" run pause "$RUN_ID" --workspace "$TMP/project" --daemon --json >/dev/null
for _ in $(seq 1 100); do
  "$ROOT/bin/takt" run summary "$RUN_ID" --workspace "$TMP/project" --daemon --json > "$TMP/paused.json"
  STATUS="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["result"]["status"])' "$TMP/paused.json")"
  [ "$STATUS" = paused ] && break
  sleep 0.05
done
[ "$STATUS" = paused ] || { cat "$TMP/paused.json" >&2; exit 1; }
[ ! -f "$TMP/project/autonomous-result.txt" ]
"$ROOT/bin/takt" runs --active --root-only --workspace "$TMP/project" --daemon --json > "$TMP/runs.json"
"$ROOT/bin/takt" attention --workspace "$TMP/project" --daemon --json > "$TMP/attention.json"
python3 - "$TMP/runs.json" "$TMP/attention.json" "$RUN_ID" <<'PY'
import json,sys
runs=json.load(open(sys.argv[1]))['result']['runs']
attention=json.load(open(sys.argv[2]))['result']['attention']
rid=sys.argv[3]
assert any(x['id']==rid and x['effective_status']=='paused' for x in runs), runs
assert any(x.get('run',{}).get('id')==rid and x['reason']=='paused' for x in attention), attention
PY
"$ROOT/bin/takt" run resume "$RUN_ID" --workspace "$TMP/project" --daemon --json >/dev/null
for _ in $(seq 1 100); do
  "$ROOT/bin/takt" run summary "$RUN_ID" --workspace "$TMP/project" --daemon --json > "$TMP/completed.json"
  STATUS="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["result"]["status"])' "$TMP/completed.json")"
  [ "$STATUS" = completed ] && break
  sleep 0.05
done
[ "$STATUS" = completed ] || { cat "$TMP/completed.json" >&2; exit 1; }
[ "$(cat "$TMP/project/autonomous-result.txt")" = done ]

# Retry preserves the Run and repeats only the failed remainder.
"$ROOT/bin/takt" run "$TMP/project/retry.yaml" --config "$TMP/project/.takt/config.yaml" --workspace "$TMP/project" --daemon --json > "$TMP/retry-start.json"
RETRY_ID="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["result"]["run_id"])' "$TMP/retry-start.json")"
for _ in $(seq 1 100); do
  "$ROOT/bin/takt" run summary "$RETRY_ID" --workspace "$TMP/project" --daemon --json > "$TMP/retry-failed.json"
  STATUS="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["result"]["status"])' "$TMP/retry-failed.json")"
  [ "$STATUS" = failed ] && break
  sleep 0.05
done
[ "$STATUS" = failed ]
touch "$TMP/project/retry-gate"
"$ROOT/bin/takt" run retry "$RETRY_ID" --workspace "$TMP/project" --daemon --json > "$TMP/retried.json"
for _ in $(seq 1 100); do
  "$ROOT/bin/takt" run summary "$RETRY_ID" --workspace "$TMP/project" --daemon --json > "$TMP/retried-summary.json"
  STATUS="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["result"]["status"])' "$TMP/retried-summary.json")"
  [ "$STATUS" = completed ] && break
  sleep 0.05
done
[ "$STATUS" = completed ] || { cat "$TMP/retried-summary.json" >&2; exit 1; }
python3 - "$TMP/retried-summary.json" <<'PY'
import json,sys
r=json.load(open(sys.argv[1]))['result']
assert r['status']=='completed', r
assert r['operator_retry_count']==1, r
PY
# Abandon is distinct from cancellation and remains in history.
"$ROOT/bin/takt" run "$TMP/project/abandon.yaml" --config "$TMP/project/.takt/config.yaml" --workspace "$TMP/project" --daemon --json > "$TMP/abandon-start.json"
ABANDON_ID="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["result"]["run_id"])' "$TMP/abandon-start.json")"
sleep 0.05
"$ROOT/bin/takt" run abandon "$ABANDON_ID" --reason 'operator test' --workspace "$TMP/project" --daemon --json >/dev/null
for _ in $(seq 1 100); do
  "$ROOT/bin/takt" run summary "$ABANDON_ID" --workspace "$TMP/project" --daemon --json > "$TMP/abandoned.json"
  STATUS="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["result"]["status"])' "$TMP/abandoned.json")"
  [ "$STATUS" = abandoned ] && break
  sleep 0.05
done
[ "$STATUS" = abandoned ] || { cat "$TMP/abandoned.json" >&2; exit 1; }

# Notifications are durable, deduplicated and acknowledgeable.
"$ROOT/bin/takt" notify dispatch --workspace "$TMP/project" --json >/dev/null
"$ROOT/bin/takt" notify list --unread --workspace "$TMP/project" --daemon --json > "$TMP/notices.json"
NOTICE_ID="$(python3 - "$TMP/notices.json" "$RUN_ID" <<'PY'
import json,sys
items=json.load(open(sys.argv[1]))['result']['notifications']
rid=sys.argv[2]
item=next(x for x in items if x.get('run_id')==rid and x['event']=='run.completed')
assert item['command'].endswith(rid), item
print(item['id'])
PY
)"
"$ROOT/bin/takt" notify ack "$NOTICE_ID" --workspace "$TMP/project" --daemon --json >/dev/null

echo 'autonomous run operations contract: PASS'
