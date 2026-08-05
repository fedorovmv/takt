#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
REPO="$TMP/repo"
mkdir -p "$REPO"
git -C "$REPO" init -q
git -C "$REPO" config user.email takt@example.invalid
git -C "$REPO" config user.name 'Takt Contract'
cat > "$REPO/workflow.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: isolated-contract
worktree:
  enabled: true
  cleanup: on_success
nodes:
  - id: change
    bash: printf 'isolated\n' > generated.txt
YAML
cat > "$REPO/config.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
YAML
git -C "$REPO" add workflow.yaml config.yaml
git -C "$REPO" commit -q -m definitions
result="$($ROOT/bin/takt run "$REPO/workflow.yaml" --config "$REPO/config.yaml" --workspace "$REPO" --json)"
fields=()
while IFS= read -r line; do
  fields+=("$line")
done < <(python3 - "$result" <<'PY'
import json,sys
v=json.loads(sys.argv[1])['result']
print(v['id'])
print(v['worktree']['path'])
print(v['worktree']['branch'])
print(v['worktree']['retained_reason'])
PY
)
run_id="${fields[0]}"
worktree_path="${fields[1]}"
branch="${fields[2]}"
reason="${fields[3]}"
[ "$reason" = "uncommitted_changes" ]
[ ! -e "$REPO/generated.txt" ]
[ "$(cat "$worktree_path/generated.txt")" = "isolated" ]
git -C "$REPO" branch --list "$branch" | grep -q "$branch"
list="$($ROOT/bin/takt worktree list --workspace "$REPO" --json)"
grep -q "$run_id" <<<"$list"
$ROOT/bin/takt worktree remove "$run_id" --workspace "$REPO" --force --json >/dev/null
[ ! -e "$worktree_path" ]
$ROOT/bin/takt worktree prune --workspace "$REPO" --json >/dev/null
printf '%s\n' 'git worktree contract: PASS'
