#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

go test ./internal/assistant -run '^TestOpenCode(AdapterContract|RunPreservesContextPriorityWithRealOverflow)$' -count=1
go test -race ./internal/assistant -run '^TestOpenCode(AdapterContract|RunPreservesContextPriorityWithRealOverflow)$' -count=1
go test ./internal/runtime -run '^TestOpenCode(AssistantResumesSessionAcrossRetry|TimeoutPreservesProviderDiagnostics)$' -count=1

if [[ "${TAKT_OPENCODE_SMOKE:-0}" == "1" ]]; then
  go test ./internal/assistant -run '^TestOpenCodeAdapterOptInSmoke$' -count=1
fi

echo 'OpenCode adapter contract suite: PASS'
