#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
cat > "$TMP/stubs.d.ts" <<'DTS'
declare const process: { cwd(): string }
declare module "node:child_process" { export const execFile: any }
declare module "node:util" { export const promisify: any }
declare module "@mariozechner/pi-coding-agent" { export type ExtensionAPI = any; export type ExtensionContext = any }
declare module "@opencode-ai/plugin" { export const Plugin: any }
DTS
cat > "$TMP/tsconfig.json" <<EOF2
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "strict": false,
    "skipLibCheck": true,
    "noEmit": true
  },
  "files": [
    "$TMP/stubs.d.ts",
    "$ROOT/integrations/coding-agent-host-control/pi/index.ts",
    "$ROOT/integrations/coding-agent-host-control/opencode/index.ts"
  ]
}
EOF2
tsc -p "$TMP/tsconfig.json"
echo 'coding-agent host integrations TypeScript: PASS'
