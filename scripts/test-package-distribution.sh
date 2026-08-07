#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export HOME="$tmp/home"
mkdir -p "$HOME" "$tmp/bin" "$tmp/project" "$tmp/pkg"
go build -o "$tmp/bin/takt" ./cmd/takt

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

echo "portable package distribution contract: PASS"
