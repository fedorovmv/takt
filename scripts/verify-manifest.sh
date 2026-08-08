#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MANIFEST="$ROOT/MANIFEST.sha256"
[ -f "$MANIFEST" ] || { echo 'MANIFEST.sha256 missing' >&2; exit 1; }
PY=""
if command -v python3 >/dev/null 2>&1; then PY=python3; elif command -v python >/dev/null 2>&1; then PY=python; else echo 'python3 or python required for manifest verification' >&2; exit 1; fi
"$PY" - "$ROOT" <<'PY'
import hashlib, pathlib, sys
root=pathlib.Path(sys.argv[1])
manifest=root/'MANIFEST.sha256'
expected={}
for line in manifest.read_text().splitlines():
    if not line.strip(): continue
    digest, rel=line.split(None,1); rel=rel.strip()
    if rel.startswith('./'): rel=rel[2:]
    if rel in expected: raise SystemExit(f'duplicate manifest entry: {rel}')
    expected[rel]=digest
actual=[]
for p in root.rglob('*'):
    if not p.is_file(): continue
    rel=p.relative_to(root).as_posix()
    if rel=='MANIFEST.sha256' or rel.startswith('.git/') or rel.startswith('bin/'): continue
    actual.append(rel)
    name=p.name
    if name.endswith(('.tmp','.swp','.bak','~')) or name.startswith('.#'):
        raise SystemExit(f'packaging hygiene violation: {rel}')
actual=set(actual)
missing=sorted(set(expected)-actual); extra=sorted(actual-set(expected))
if missing or extra:
    raise SystemExit(f'manifest file set mismatch missing={missing[:10]} extra={extra[:10]}')
for rel,digest in sorted(expected.items()):
    got=hashlib.sha256((root/rel).read_bytes()).hexdigest()
    if got!=digest: raise SystemExit(f'manifest checksum mismatch: {rel}')
print(f'manifest verification: PASS ({len(expected)} files)')
PY
