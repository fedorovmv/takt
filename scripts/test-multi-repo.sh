#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/work"
go build -o "$tmp/bin/takt" ./cmd/takt
go build -o "$tmp/bin/takt-fake-assistant" ./cmd/takt-fake-assistant
go build -o "$tmp/bin/takt-fake-domain-adapter" ./cmd/takt-fake-domain-adapter
export PATH="$tmp/bin:$PATH"
export TAKT_FAKE_ADAPTER_STATE="$tmp/adapter-state"

for repo in api client service; do
  mkdir -p "$tmp/work/$repo"
  git -C "$tmp/work/$repo" init -q
  git -C "$tmp/work/$repo" config user.email test@example.com
  git -C "$tmp/work/$repo" config user.name Test
  printf 'base\n' > "$tmp/work/$repo/README.md"
  git -C "$tmp/work/$repo" add .
  git -C "$tmp/work/$repo" commit -qm base
done

"$tmp/bin/takt" init code --dir "$tmp/work" >/dev/null
cat > "$tmp/work/.takt/config.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
default_assistant: fixture
models:
  routing:
    provider: local
    id: route
  implementation:
    provider: local
    id: implementation
  review:
    provider: local
    id: review
assistants:
  fixture:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: [takt-fake-assistant, --case, multi-repo]
    capabilities: [tool_policy, skills, mcp, sandbox_filesystem]
adapters:
  scm:
    domain: scm
    transport: process
    argv: [takt-fake-domain-adapter, --domain=scm]
    timeout: 10s
YAML
cat > "$tmp/work/.takt/workspace.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workspace
repositories:
  - id: api
    path: api
  - id: client
    path: client
    depends_on: [api]
  - id: service
    path: service
    depends_on: [client]
YAML

"$tmp/bin/takt" plan 'update API, client and service together' --workspace "$tmp/work" --json > "$tmp/plan.json"
plan_id="$(python - "$tmp/plan.json" <<'PY'
import json,sys
x=json.load(open(sys.argv[1]))['result']
assert x['decision']=='planned'
assert x['record']['merge_order']==['api','client','service']
assert x['record']['repository_catalog_fingerprint'].startswith('sha256:')
print(x['plan_id'])
PY
)"
"$tmp/bin/takt" execute "$plan_id" --confirm --workspace "$tmp/work" --json > "$tmp/result.json"
python - "$tmp/result.json" <<'PY'
import json,os,sys
record=json.load(open(sys.argv[1]))['result']
assert record['status']=='completed', record.get('last_error')
assert record['merge_order']==['api','client','service']
repos=record['repository_executions']
assert set(repos)=={'api','client','service'}
for name, value in repos.items():
    assert value['status']=='completed'
    assert value['candidate_sha'].startswith('sha256:')
    assert value['branch'].startswith('takt/')
    assert value['change_output']
    assert value['evidence']['verdict']['status']=='pass'
    assert os.path.exists(os.path.join(value['execution_workspace'],'takt-multi-repo.txt'))
assert record['evidence']['verdict']['status']=='pass'
PY
for repo in api client service; do
  test ! -e "$tmp/work/$repo/takt-multi-repo.txt"
done
# The fake SCM process appends one durable receipt per publish action to a
# single state file. Prove that all three repository changes were published.
test "$(wc -l < "$tmp/adapter-state" | tr -d ' ')" -ge 3

echo "multi-repo dynamic workflow contract: PASS"
