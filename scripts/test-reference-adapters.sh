#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/qwen-work" "$tmp/scm-work"
go build -o "$tmp/bin/takt" ./cmd/takt
go build -o "$tmp/bin/qwen-takt-adapter" ./cmd/qwen-takt-adapter
go build -o "$tmp/bin/takt-github-scm-adapter" ./cmd/takt-github-scm-adapter

# Qwen Code reference wrapper: exercise the public v1alpha2 contract through
# the real Takt process transport. The fake qwen executable only replaces the
# upstream CLI so this test is deterministic and needs no model credentials.
cat > "$tmp/bin/qwen" <<'SH'
#!/bin/sh
set -eu
printf '%s\n' "$@" > "$QWEN_FIXTURE_ARGS"
session=qwen-session-1
prev=""
for arg in "$@"; do
  if [ "$prev" = "--resume" ]; then session="$arg"; fi
  prev="$arg"
done
printf '{"type":"system","subtype":"session_start","session_id":"%s","model":"qwen-reference"}\n' "$session"
printf '{"type":"assistant","session_id":"%s","message":{"content":[{"type":"text","text":"reference wrapper completed"}]}}\n' "$session"
printf '{"type":"result","subtype":"success","session_id":"%s","is_error":false,"result":"reference wrapper completed","usage":{"input_tokens":11,"output_tokens":5}}\n' "$session"
SH
chmod +x "$tmp/bin/qwen"
cat > "$tmp/qwen-work/config.yaml" <<EOFQ
apiVersion: takt/v1alpha1
kind: Config
models:
  default:
    provider: qwen
    id: qwen3-coder
assistants:
  qwen-reference:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: [$tmp/bin/qwen-takt-adapter]
    env:
      QWEN_TAKT_QWEN_BINARY: $tmp/bin/qwen
      QWEN_FIXTURE_ARGS: $tmp/qwen-args.txt
    capabilities: [agent_events_v2, session_events, usage_events]
EOFQ
cat > "$tmp/qwen-work/workflow.yaml" <<'EOFQ'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: qwen-reference-adapter

defaults:
  assistant: qwen-reference
  model: default
  session: fresh
nodes:
  - id: execute
    prompt: Return a short result.
EOFQ
"$tmp/bin/takt" run "$tmp/qwen-work/workflow.yaml" --config "$tmp/qwen-work/config.yaml" --workspace "$tmp/qwen-work" --json > "$tmp/qwen-run.json"
grep -q '"status": "completed"' "$tmp/qwen-run.json"
grep -q 'reference wrapper completed' "$tmp/qwen-run.json"
grep -q -- '--safe-mode' "$tmp/qwen-args.txt"
grep -q -- '--output-format' "$tmp/qwen-args.txt"

# GitHub SCM reference adapter: emulate the gh transport failing after a PR
# was created. Takt must reconcile by the hashed idempotency marker and finish
# without a second mutation.
git -C "$tmp/scm-work" init -q
git -C "$tmp/scm-work" remote add origin git@github.com:acme/reference.git
cat > "$tmp/bin/gh" <<'SH'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$GH_FIXTURE_LOG"
if [ "$1" = "pr" ] && [ "$2" = "create" ]; then
  prev=""
  for arg in "$@"; do
    if [ "$prev" = "--body" ]; then printf '%s' "$arg" > "$GH_FIXTURE_BODY"; fi
    prev="$arg"
  done
  echo 'transport lost after mutation' >&2
  exit 1
fi
if [ "$1" = "pr" ] && [ "$2" = "list" ]; then
  body=$(cat "$GH_FIXTURE_BODY")
  printf '[{"number":17,"url":"https://github.com/acme/reference/pull/17","body":"%s"}]\n' "$body"
  exit 0
fi
printf '{}\n'
SH
chmod +x "$tmp/bin/gh"
cat > "$tmp/scm-work/config.yaml" <<EOFSCM
apiVersion: takt/v1alpha1
kind: Config
adapters:
  scm:
    domain: scm
    transport: process
    argv: [$tmp/bin/takt-github-scm-adapter]
    env:
      TAKT_GITHUB_GH_BINARY: $tmp/bin/gh
      GH_FIXTURE_LOG: $tmp/gh.log
      GH_FIXTURE_BODY: $tmp/gh-body.txt
    timeout: 10s
EOFSCM
cat > "$tmp/scm-work/workflow.yaml" <<'EOFSCM'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: github-reference-reconcile
nodes:
  - id: publish
    adapter:
      name: scm
      operation: change.create
      input: |
        {"title":"Reference change","head":"takt/reference"}
    side_effect:
      mode: reconcile
      idempotency_key: reference-change-1
EOFSCM
"$tmp/bin/takt" adapter doctor scm --workspace "$tmp/scm-work" --config "$tmp/scm-work/config.yaml" --json > "$tmp/scm-doctor.json"
grep -q 'change.create' "$tmp/scm-doctor.json"
"$tmp/bin/takt" run "$tmp/scm-work/workflow.yaml" --config "$tmp/scm-work/config.yaml" --workspace "$tmp/scm-work" --json > "$tmp/scm-run.json"
grep -q '"status": "completed"' "$tmp/scm-run.json"
grep -q '"reconcile_status": "applied"' "$tmp/scm-run.json"
grep -q 'https://github.com/acme/reference/pull/17' "$tmp/scm-run.json"
[[ "$(grep -c '^pr create' "$tmp/gh.log")" -eq 1 ]]
[[ "$(grep -c '^pr list' "$tmp/gh.log")" -eq 1 ]]
if grep -q 'reference-change-1' "$tmp/gh-body.txt"; then
  echo "raw Takt idempotency key leaked into GitHub body" >&2
  exit 1
fi
grep -q 'takt-idempotency:' "$tmp/gh-body.txt"

# Reference implementations may only consume public SDK packages.
if grep -R --include='*.go' --exclude='*_test.go' '"takt/internal/' reference cmd/qwen-takt-adapter cmd/takt-github-scm-adapter; then
  echo "reference adapter imports Takt internal package" >&2
  exit 1
fi

echo "reference external adapters contract: PASS"
