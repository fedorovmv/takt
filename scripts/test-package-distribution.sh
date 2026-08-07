#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export HOME="$tmp/home"
mkdir -p "$HOME" "$tmp/bin" "$tmp/project" "$tmp/pkg"
go build -o "$tmp/bin/takt" ./cmd/takt
go build -o "$tmp/bin/takt-fake-assistant" ./cmd/takt-fake-assistant
export PATH="$tmp/bin:$PATH"

cat > "$tmp/pkg/workflow.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: portable-extra
nodes:
  - id: result
    prompt: Return JSON summary.
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
YAML
cat > "$tmp/pkg/package.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: BlockPackage
metadata:
  name: portable-extra
  version: 1.0.0
  scope: project
blocks:
  portable-extra:
    workflow: workflow.yaml
    output_paths: [summary]
requirements:
  takt: ">=0.1.42"
  adapters:
    - name: scm
      domain: scm
      operations: [change.get]
      level: preferred
YAML

"$tmp/bin/takt" init code --dir "$tmp/project" --json > "$tmp/init.json"
"$tmp/bin/takt" package install "$tmp/pkg" --scope project --workspace "$tmp/project" --json > "$tmp/install.json"
grep -q 'portable-extra' "$tmp/install.json"
test -f "$tmp/project/.takt/takt.lock.json"

"$tmp/bin/takt" package list --workspace "$tmp/project" --json > "$tmp/list.json"
grep -q 'portable-extra' "$tmp/list.json"
"$tmp/bin/takt" package doctor --workspace "$tmp/project" --json > "$tmp/doctor.json"
grep -q '"status": "ready"' "$tmp/doctor.json"
grep -q '"adapter_preflight"' "$tmp/doctor.json"
grep -q '"available": false' "$tmp/doctor.json"

# Installed packages are automatically part of the resolved profile catalog.
"$tmp/bin/takt" block list --profile code --workspace "$tmp/project" --json > "$tmp/blocks.json"
grep -q 'portable-extra' "$tmp/blocks.json"

installed="$tmp/project/.takt/packages/project/portable-extra/1.0.0/workflow.yaml"
echo corrupt > "$installed"
if "$tmp/bin/takt" package doctor --workspace "$tmp/project" --json >/dev/null 2>&1; then
  echo "doctor accepted corrupted installed content" >&2
  exit 1
fi
"$tmp/bin/takt" package sync --workspace "$tmp/project" --json > "$tmp/sync.json"
grep -q '"status": "ready"' "$tmp/sync.json"
"$tmp/bin/takt" package doctor --workspace "$tmp/project" --json >/dev/null

"$tmp/bin/takt" package uninstall portable-extra --scope project --workspace "$tmp/project" --json > "$tmp/remove.json"
if "$tmp/bin/takt" block list --profile code --workspace "$tmp/project" --json | grep -q 'portable-extra'; then
  echo "uninstalled package remains in profile catalog" >&2
  exit 1
fi

# The shipped example is part of the executable release contract, not just
# documentation. Install the real directory and run its workflow from the
# locked copy so commands/, scripts/, skills/ and MCP resolution are exercised.
cat > "$tmp/project/.takt/config.yaml" <<'YAML'
apiVersion: takt/v1alpha1
kind: Config
default_assistant: fixture
models:
  routing:
    provider: local
    id: routing
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
    argv: [takt-fake-assistant, --case, portable-package]
    capabilities: [skills, mcp]
YAML
"$tmp/bin/takt" package install "$root/examples/portable-package" --scope project --workspace "$tmp/project" --json > "$tmp/example-install.json"
example_root="$tmp/project/.takt/packages/project/portable-review/1.0.0"
test -x "$example_root/scripts/inventory.sh"
test -f "$example_root/commands/package-review.md"
test -f "$example_root/skills/review/SKILL.md"
test -f "$example_root/mcp.json"
"$tmp/bin/takt" run "$example_root/workflow.yaml" --workspace "$tmp/project" --config "$tmp/project/.takt/config.yaml" --json > "$tmp/example-run.json"
grep -q '"status": "completed"' "$tmp/example-run.json"
grep -q 'portable package review passed' "$tmp/example-run.json"

echo "portable package distribution contract: PASS"
