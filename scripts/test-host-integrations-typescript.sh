#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
TSC="${TSC:-}"
if [[ -z "$TSC" && -x "$ROOT/integrations/coding-agent-host-control/node_modules/.bin/tsc" ]]; then
  TSC="$ROOT/integrations/coding-agent-host-control/node_modules/.bin/tsc"
fi
if [[ -z "$TSC" ]] && command -v tsc >/dev/null 2>&1; then
  TSC="$(command -v tsc)"
fi
if [[ -z "$TSC" ]]; then
  if [[ "${TAKT_REQUIRE_TYPESCRIPT:-0}" == "1" ]]; then
    echo "TypeScript compiler is required; install pinned devDependencies" >&2
    exit 1
  fi
  echo 'coding-agent host integrations TypeScript: SKIP (install pinned devDependencies or set TSC)'
  exit 0
fi
cat > "$TMP/tsconfig.json" <<EOF2
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "strict": true,
    "skipLibCheck": false,
    "rootDir": "$ROOT/integrations/coding-agent-host-control",
    "outDir": "$TMP/out"
  },
  "files": [
    "$ROOT/integrations/coding-agent-host-control/contracts/pi-0.73.1.d.ts",
    "$ROOT/integrations/coding-agent-host-control/contracts/opencode-1.18.14.d.ts",
    "$ROOT/integrations/coding-agent-host-control/contracts/opencode-blocking-contract.mts",
    "$ROOT/integrations/coding-agent-host-control/contracts/opencode-entrypoint-contract.mts",
    "$ROOT/integrations/coding-agent-host-control/pi/index.ts",
    "$ROOT/integrations/coding-agent-host-control/opencode/index.ts"
  ]
}
EOF2
"$TSC" -p "$TMP/tsconfig.json"
cat > "$TMP/takt" <<'EOF2'
#!/bin/sh
case "$*" in
  *"daemon start"*)
    exit 0
    ;;
  *"host begin"*"-- goal --workspace"*)
    printf '%s\n' '{"ok":false,"error":{"message":"common flags leaked into goal"}}'
    exit 1
    ;;
  *"host begin"*)
    printf '%s\n' '{"ok":true,"result":{"session":{"id":"host","status":"preview","plan_id":"plan"},"plan":{"preview":"preview marker"}}}'
    exit 0
    ;;
  *"--host-session tool-contract"*)
    printf '%s\n' '{"ok":true,"result":{"session":{"id":"host","status":"preview","plan_id":"plan"},"plan":{}}}'
    exit 0
    ;;
  *"host guard-tool"*)
    printf '%s\n' '{"ok":true,"result":{"allowed":false,"reason":"policy denied"}}'
    exit 0
    ;;
esac
printf '%s\n' '{"ok":false,"error":{"message":"not found"}}'
exit 1
EOF2
chmod +x "$TMP/takt"
mkdir "$TMP/workspace"
(
  cd "$TMP/workspace"
  PATH="$TMP:$PATH" node "$TMP/out/contracts/opencode-blocking-contract.mjs"
)
echo 'coding-agent host integrations TypeScript: PASS'
