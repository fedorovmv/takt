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
cat > "$PROJECT/PLAN.md" <<'PLAN'
# Fixture plan

Update app.txt and validate the resulting change.
PLAN
cat > "$PROJECT/AGENTS.md" <<'AGENTS'
# Fixture rules

Keep changes scoped to app.txt and evidence files outside Git.
AGENTS
git -C "$PROJECT" add app.txt PLAN.md AGENTS.md
git -C "$PROJECT" commit -m initial >/dev/null
git -C "$PROJECT" remote add origin "$REMOTE"
git -C "$PROJECT" push -u origin main >/dev/null

"$ROOT/bin/takt" init code --dir "$PROJECT" --json >/dev/null
cat > "$PROJECT/.takt/config.yaml" <<CFG
apiVersion: takt/v1alpha1
kind: Config
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
CFG
git -C "$PROJECT" add .takt
git -C "$PROJECT" commit -m 'add Takt deep workflow profile' >/dev/null
git -C "$PROJECT" push origin main >/dev/null

run_until_terminal() {
  local selector="$1"
  local input_file="$2"
  local output status run_id waiting_node
  output="$("$ROOT/bin/takt" run "$selector" --workspace "$PROJECT" --input "$input_file" --json)"
  while :; do
    status="$(python3 -c 'import json,sys; v=json.loads(sys.argv[1]); v=v.get("result",v); print(v["status"])' "$output")"
    [ "$status" = "waiting" ] || break
    read -r run_id waiting_node <<EOFSTATE
$(python3 -c 'import json,sys; v=json.loads(sys.argv[1]); v=v.get("result",v); w=v.get("waiting") or {}; print(v["id"], w.get("node_id", ""))' "$output")
EOFSTATE
    [ -n "$waiting_node" ] || {
      echo "waiting Run has no node_id for $selector" >&2
      return 1
    }
    output="$("$ROOT/bin/takt" answer "$run_id" "$waiting_node" --workspace "$PROJECT" --value ready --json)"
  done
  printf '%s' "$output"
}

assert_artifacts() {
  local output="$1"
  shift
  python3 -c '
import json,sys
v=json.loads(sys.argv[1]); v=v.get("result",v)
assert v["status"] == "completed", v
actual={item["type"] for item in v.get("artifacts",[])}
missing=set(sys.argv[2:])-actual
assert not missing,(missing,actual)
' "$output" "$@"
}

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
fix_result="$(run_until_terminal code:fix-github-issue "$TMP/fix.json")"
assert_artifacts "$fix_result" issue-intake investigation reproduction fix-plan implementation-report validation-report pr-metadata workflow-summary
[ "$(cat "$TMP/gh-state/pr-count")" = "1" ]
git --git-dir="$REMOTE" show-ref --heads | grep -q 'refs/heads/takt/'

# Recovery scenario: first validation checkpoint fails, recovery removes the marker,
# revalidation succeeds, and the Run still reaches a PR with preserved evidence.
git -C "$PROJECT" checkout main >/dev/null
export FAKE_VALIDATION_FAIL_FILE="$TMP/force-validation-failure"
printf 'fail once\n' > "$FAKE_VALIDATION_FAIL_FILE"
cat > "$TMP/plan.json" <<JSON
{
  "repository": "acme/repo",
  "plan_path": "$PROJECT/PLAN.md",
  "base_branch": "main",
  "draft_pr": true,
  "validation_commands": ["test -s app.txt"]
}
JSON
plan_result="$(run_until_terminal code:plan-to-pr "$TMP/plan.json")"
assert_artifacts "$plan_result" plan-confirmation implementation-report recovery-report validation-report pr-metadata workflow-summary
[ ! -e "$FAKE_VALIDATION_FAIL_FILE" ]
[ "$(cat "$TMP/gh-state/pr-count")" = "2" ]
unset FAKE_VALIDATION_FAIL_FILE

# The remaining four deep workflows also receive a full Run on the Git fixture.
git -C "$PROJECT" checkout main >/dev/null
cat > "$TMP/idea.json" <<'JSON'
{
  "repository": "acme/repo",
  "idea": "Improve app.txt with a documented fixture change",
  "base_branch": "main",
  "draft_pr": true,
  "validation_commands": ["test -s app.txt"],
  "scope_limits": ["Only app.txt"]
}
JSON
idea_result="$(run_until_terminal code:idea-to-pr "$TMP/idea.json")"
assert_artifacts "$idea_result" git-state idea-research plan implementation-report validation-report pr-metadata workflow-summary

git -C "$PROJECT" checkout main >/dev/null
cat > "$TMP/review.json" <<'JSON'
{
  "repository": "acme/repo",
  "pr_number": 1,
  "fix_findings": false,
  "validation_commands": ["test -s app.txt"]
}
JSON
review_result="$(run_until_terminal code:smart-pr-review "$TMP/review.json")"
assert_artifacts "$review_result" review-scope workflow-summary

git -C "$PROJECT" checkout main >/dev/null
cat > "$TMP/piv.json" <<'JSON'
{
  "repository": "acme/repo",
  "request": "Plan, implement and validate a scoped app.txt update",
  "base_branch": "main",
  "validation_commands": ["test -s app.txt"],
  "max_review_rounds": 2
}
JSON
piv_result="$(run_until_terminal code:piv-loop "$TMP/piv.json")"
assert_artifacts "$piv_result" git-state exploration plan implementation-report validation-report workflow-summary

git -C "$PROJECT" checkout main >/dev/null
cat > "$TMP/ralph.json" <<JSON
{
  "repository": "acme/repo",
  "prd_path": "$PROJECT/PLAN.md",
  "request": "Execute the fixture backlog",
  "base_branch": "main",
  "draft_pr": true,
  "validation_commands": ["test -s app.txt"],
  "max_stories": 3
}
JSON
ralph_result="$(run_until_terminal code:ralph-dag "$TMP/ralph.json")"
assert_artifacts "$ralph_result" git-state ralph-backlog ralph-progress validation-report pr-metadata ralph-summary

[ "$(cat "$TMP/gh-state/pr-count")" = "4" ]

# A blocked subject checkpoint is a control-flow decision, not merely report text:
# no plan, implementation, validation or PR may run after research blocks.
git -C "$PROJECT" checkout main >/dev/null
blocked_pr_count="$(cat "$TMP/gh-state/pr-count")"
blocked_app_sha="$(git -C "$PROJECT" hash-object app.txt)"
export FAKE_BLOCK_PHASE=idea-research
set +e
blocked_error="$("$ROOT/bin/takt" run code:idea-to-pr --workspace "$PROJECT" --input "$TMP/idea.json" --json 2>&1)"
blocked_rc=$?
set -e
unset FAKE_BLOCK_PHASE
[ "$blocked_rc" -ne 0 ] || {
  echo 'blocked checkpoint unexpectedly produced a successful Run' >&2
  exit 1
}
blocked_run_id="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["error"]["details"]["run_id"])' "$blocked_error")"
blocked_result="$("$ROOT/bin/takt" status "$blocked_run_id" --workspace "$PROJECT" --json)"
python3 -c '
import json,sys
v=json.loads(sys.argv[1]); v=v.get("result",v)
assert v.get("status") == "failed", v
nodes=v.get("nodes",{})
research=nodes.get("research",{})
assert research.get("status") == "completed", research
research_output=research.get("output",{})
if isinstance(research_output,str):
    research_output=json.loads(research_output)
assert research_output.get("status") == "blocked", research
for node_id in ("plan","approve-plan","implement","validate","create-pr"):
    state=nodes.get(node_id,{})
    assert state.get("status") != "completed", (node_id,state)
artifact_types={item.get("type") for item in v.get("artifacts",[])}
for forbidden in ("plan","implementation-report","validation-report","pr-metadata"):
    assert forbidden not in artifact_types,(forbidden,artifact_types)
' "$blocked_result"
[ "$(cat "$TMP/gh-state/pr-count")" = "$blocked_pr_count" ]
[ "$(git -C "$PROJECT" hash-object app.txt)" = "$blocked_app_sha" ]

# Exact input contracts fail before an assistant or Git command is started.
cat > "$TMP/invalid.json" <<'JSON'
{"repository":"acme/repo","issue_number":1}
JSON
if "$ROOT/bin/takt" run code:fix-github-issue --workspace "$PROJECT" --input "$TMP/invalid.json" --json >/dev/null 2>"$TMP/invalid.err"; then
  echo 'invalid deep-workflow input unexpectedly succeeded' >&2
  exit 1
fi
grep -q 'workflow input' "$TMP/invalid.err"

printf '%s\n' 'deep code workflows: PASS'
