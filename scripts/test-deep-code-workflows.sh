#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
PROJECT="$TMP/project"
REMOTE="$TMP/remote.git"
mkdir -p "$PROJECT" "$TMP/bin" "$TMP/gh-state"
cp "$ROOT/scripts/fixtures/fake-gh" "$TMP/bin/gh"
export PATH="$TMP/bin:$PATH"
export FAKE_GH_STATE_DIR="$TMP/gh-state"

git init --bare "$REMOTE" >/dev/null
git init -b main "$PROJECT" >/dev/null
git -C "$PROJECT" config user.name 'Takt Fixture'
git -C "$PROJECT" config user.email 'takt@example.test'
printf 'initial\n' > "$PROJECT/app.txt"
git -C "$PROJECT" add app.txt
git -C "$PROJECT" commit -m initial >/dev/null
git -C "$PROJECT" remote add origin "$REMOTE"
git -C "$PROJECT" push -u origin main >/dev/null

"$ROOT/bin/takt" init code --dir "$PROJECT" --json >/dev/null
cat > "$PROJECT/.takt/config.yaml" <<CFG
apiVersion: takt/v1alpha1
kind: Config
default_assistant: opencode
models:
  routing:
    provider: fixture
    id: routing
  review:
    provider: fixture
    id: review
  implementation:
    provider: fixture
    id: implementation
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

git -C "$PROJECT" add .takt
git -C "$PROJECT" commit -m 'add Takt code profile' >/dev/null
git -C "$PROJECT" push origin main >/dev/null

cat > "$TMP/fix.json" <<'JSON'
{
  "repository": "acme/repo",
  "issue_number": 1,
  "base_branch": "main",
  "draft_pr": true,
  "validation_commands": ["test -s app.txt"],
  "scope_limits": ["Only app.txt"]
}
JSON

output="$("$ROOT/bin/takt" run code:fix-github-issue --workspace "$PROJECT" --input "$TMP/fix.json" --json)"
while :; do
  read -r status run_id waiting_node <<EOF_STATE
$(python3 -c 'import json,sys; v=json.loads(sys.argv[1]); v=v.get("result",v); w=v.get("waiting") or {}; print(v["status"], v["id"], w.get("node_id", ""))' "$output")
EOF_STATE
  [ "$status" = "waiting" ] || break
  [ -n "$waiting_node" ] || { echo 'waiting Run has no node_id' >&2; exit 1; }
  output="$("$ROOT/bin/takt" answer "$run_id" "$waiting_node" --workspace "$PROJECT" --value ready --json)"
done

python3 -c '
import json,sys
v=json.loads(sys.argv[1]); v=v.get("result",v)
assert v["status"] == "completed", v
actual={item["type"] for item in v.get("artifacts",[])}
expected={"issue-intake","investigation","reproduction","fix-plan","implementation-report","validation-report","pr-metadata","workflow-summary"}
assert expected <= actual, (expected-actual, actual)
' "$output"

[ "$(cat "$TMP/gh-state/pr-count")" = "1" ]
git --git-dir="$REMOTE" show-ref --heads | grep -q 'refs/heads/takt/'

# Input-schema rejection must happen before an assistant/Git side effect.
cat > "$TMP/invalid.json" <<'JSON'
{"repository":"acme/repo","issue_number":1}
JSON
before_pr_count="$(cat "$TMP/gh-state/pr-count")"
if "$ROOT/bin/takt" run code:fix-github-issue --workspace "$PROJECT" --input "$TMP/invalid.json" --json >/dev/null 2>"$TMP/invalid.err"; then
  echo 'invalid deep-workflow input unexpectedly succeeded' >&2
  exit 1
fi
grep -q 'workflow input' "$TMP/invalid.err"
[ "$(cat "$TMP/gh-state/pr-count")" = "$before_pr_count" ]

printf '%s\n' 'deep code workflows: PASS'
