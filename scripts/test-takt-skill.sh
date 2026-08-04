#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

skill="skills/takt/SKILL.md"
profile="skills/takt/assets/validated-agent-profile"

[[ -f "$skill" ]] || { echo "missing $skill" >&2; exit 1; }
grep -Fq 'name: takt' "$skill"
grep -Fq 'takt validate' "$skill"
grep -Fq 'references/patterns.md' "$skill"
grep -Fq 'opencode' "$skill"

for file in \
  README.md \
  VERSION \
  references/configuration.md \
  references/workflows.md \
  references/patterns.md \
  references/troubleshooting.md \
  assets/validated-agent-profile/.takt/config.yaml \
  assets/validated-agent-profile/.takt/workflows/basic.yaml \
  assets/validated-agent-profile/.takt/workflows/validated.yaml \
  assets/validated-agent-profile/.takt/workflows/opencode.yaml \
  assets/validated-agent-profile/.takt/commands/implement.md \
  assets/validated-agent-profile/.takt/tools/validate-result
 do
  [[ -f "skills/takt/$file" ]] || { echo "missing skills/takt/$file" >&2; exit 1; }
 done

binary="${TAKT_BIN:-$root/bin/takt}"
[[ -x "$binary" ]] || { echo "takt binary not found: $binary" >&2; exit 1; }

"$binary" validate "$profile/.takt/workflows/basic.yaml" \
  --config "$profile/.takt/config.yaml" \
  --workspace "$profile" \
  --json >/dev/null

"$binary" validate "$profile/.takt/workflows/validated.yaml" \
  --config "$profile/.takt/config.yaml" \
  --workspace "$profile" \
  --json >/dev/null

"$binary" validate "$profile/.takt/workflows/opencode.yaml" \
  --config "$profile/.takt/config.yaml" \
  --workspace "$profile" \
  --json >/dev/null

printf 'Takt authoring skill: PASS\n'
