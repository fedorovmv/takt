#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PYTHON=""
if command -v python3 >/dev/null 2>&1; then PYTHON=python3
elif command -v python >/dev/null 2>&1; then PYTHON=python
else echo 'python3 or python is required for schema registry contract' >&2; exit 1
fi
"$PYTHON" - "$ROOT" <<'PY'
import json, pathlib, re, sys
root=pathlib.Path(sys.argv[1])
schema_dir=root/'schemas'
readme=(schema_dir/'README.md').read_text(encoding='utf-8')
files=sorted(schema_dir.glob('*.schema.json'))
actual={p.name for p in files}
for p in files:
    try: data=json.loads(p.read_text(encoding='utf-8'))
    except Exception as e: raise SystemExit(f'{p.name}: invalid JSON: {e}')
    if data.get('$schema') != 'https://json-schema.org/draft/2020-12/schema':
        raise SystemExit(f'{p.name}: expected Draft 2020-12 $schema')
    refs=[]
    def walk(v):
        if isinstance(v,dict):
            for k,x in v.items():
                if k=='$ref': refs.append(x)
                walk(x)
        elif isinstance(v,list):
            for x in v: walk(x)
    walk(data)
    external=[r for r in refs if not r.startswith('#')]
    if external: raise SystemExit(f'{p.name}: external/cross-file $ref is forbidden: {external}')
missing=[name for name in sorted(actual) if f'`{name}`' not in readme]
if missing: raise SystemExit('schemas/README.md missing: '+', '.join(missing))
mentioned=set(re.findall(r'`([^`]+\.schema\.json)`', readme))
stale=sorted(mentioned-actual)
if stale: raise SystemExit('schemas/README.md contains stale entries: '+', '.join(stale))
print(f'schema registry contract: PASS ({len(files)} schemas)')
PY
