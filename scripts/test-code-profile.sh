#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/project"
cat > "$TMP/project/PLAN.md" <<'PLAN'
# Development plan

- [ ] Implement the requested change.
- [ ] Run project validation.
PLAN
"$ROOT/bin/takt" init code --dir "$TMP/project" --json >/dev/null
"$ROOT/bin/takt" validate code --workspace "$TMP/project" --json >/dev/null
[ -f "$TMP/project/.takt/profiles/code/profile.yaml" ]
[ "$(tr -d '[:space:]' < "$TMP/project/.takt/profiles/code/VERSION")" = "0.2.0" ]
[ -f "$TMP/project/.takt/profiles/code/workflows/implementation.yaml" ]
[ -f "$TMP/project/.takt/profiles/code/workflows/review.yaml" ]
[ -f "$TMP/project/.takt/config.yaml" ]
grep -q 'format: markdown' "$TMP/project/.takt/profiles/code/profile.yaml"
grep -q 'preserve_path: true' "$TMP/project/.takt/profiles/code/profile.yaml"
grep -q 'subworkflow:' "$TMP/project/.takt/profiles/code/workflow.yaml"
printf '%s\n' 'code profile contract: PASS'
