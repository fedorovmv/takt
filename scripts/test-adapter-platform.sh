#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/work"
go build -o "$tmp/bin/takt" ./cmd/takt
go build -o "$tmp/bin/takt-fake-domain-adapter" ./cmd/takt-fake-domain-adapter
export PATH="$tmp/bin:$PATH"
export TAKT_FAKE_ADAPTER_STATE="$tmp/adapter-state"

cp examples/adapter-platform/config.yaml "$tmp/work/config.yaml"
cp examples/adapter-platform/workflow.yaml "$tmp/work/workflow.yaml"

"$tmp/bin/takt" adapter doctor tracker --workspace "$tmp/work" --config config.yaml --json > "$tmp/tracker.json"
"$tmp/bin/takt" adapter doctor scm --workspace "$tmp/work" --config config.yaml --json > "$tmp/scm.json"
grep -q 'item.get' "$tmp/tracker.json"
grep -q 'change.create' "$tmp/scm.json"

"$tmp/bin/takt" validate "$tmp/work/workflow.yaml" --config "$tmp/work/config.yaml"
"$tmp/bin/takt" run "$tmp/work/workflow.yaml" --config "$tmp/work/config.yaml" --workspace "$tmp/work" --json > "$tmp/run.json"
grep -q '"status": "completed"' "$tmp/run.json"
grep -q '"domain_operation"' "$tmp/run.json"
grep -q '"reconcile_status": "applied"' "$tmp/run.json"

echo "adapter platform contract: PASS"
