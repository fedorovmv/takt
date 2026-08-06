#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/project"

"$ROOT/bin/takt" init code --dir "$TMP/project" --json >/dev/null

builtin="$TMP/project/.takt/profiles/code/workflows/blocks/package.yaml"
"$ROOT/bin/takt" block validate "$builtin" --json >"$TMP/builtin.json"
"$ROOT/bin/takt" block list --profile code --workspace "$TMP/project" --json >"$TMP/list.json"
"$ROOT/bin/takt" block describe adversarial-verify --profile code --workspace "$TMP/project" --json >"$TMP/adversarial.json"
"$ROOT/bin/takt" block validate "$ROOT/examples/corporate-block-package/package.yaml" --json >"$TMP/corporate.json"

python3 - "$TMP/list.json" "$TMP/adversarial.json" "$TMP/corporate.json" <<'PY'
import json, sys
catalog = json.load(open(sys.argv[1], encoding="utf-8"))["result"]
adversarial = json.load(open(sys.argv[2], encoding="utf-8"))["result"]
corporate = json.load(open(sys.argv[3], encoding="utf-8"))["result"]
assert len(catalog["blocks"]) == 9, catalog
assert catalog["fingerprint"]
assert any(p["name"] == "code-core" and p["scope"] == "builtin" for p in catalog["packages"])
assert adversarial["workflow_path"].endswith("dynamic-adversarial-verify.yaml")
assert adversarial["output_types"]["findings"] == "array"
assert corporate["governance"]["required_blocks"] == ["corp-validate"]
assert corporate["governance"]["branch_rules"]["prefix"] == "feature/"
assert corporate["governance"]["limits"]["max_parallel"] == 8
PY

# A project can explicitly add the corporate package to the trusted catalog.
mkdir -p "$TMP/project/.takt/packages/corporate-engineering"
cp "$ROOT/examples/corporate-block-package/"*.yaml "$TMP/project/.takt/packages/corporate-engineering/"
python3 - "$TMP/project/.takt/profiles/code/profile.yaml" <<'PY'
from pathlib import Path
import sys
p = Path(sys.argv[1])
s = p.read_text()
s = s.replace(
    "block_packages:\n  - workflows/blocks/package.yaml\n",
    "block_packages:\n  - workflows/blocks/package.yaml\n  - ../../packages/corporate-engineering/package.yaml\n",
)
p.write_text(s)
PY
"$ROOT/bin/takt" block list --profile code --workspace "$TMP/project" --json >"$TMP/merged.json"
python3 - "$TMP/merged.json" <<'PY'
import json, sys
catalog = json.load(open(sys.argv[1], encoding="utf-8"))["result"]
assert len(catalog["blocks"]) == 12, catalog
assert any(p["name"] == "corporate-engineering" and p["scope"] == "corporate" for p in catalog["packages"])
assert catalog["governance"]["required_blocks"] == ["corp-validate"]
assert catalog["governance"]["limits"]["max_child_runs"] == 48
core = next(b for b in catalog["blocks"] if b["name"] == "implement")
assert "network-unapproved" in core["policy"]["denied_tools"]
PY

printf '%s\n' 'trusted block package contract: PASS'
