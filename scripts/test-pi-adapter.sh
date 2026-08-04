#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

go test ./internal/assistant -run '^TestPi(AdapterContract|RunPreservesContextPriorityWithRealOverflow)$' -count=1
go test -race ./internal/assistant -run '^TestPi(AdapterContract|RunPreservesContextPriorityWithRealOverflow)$' -count=1
go test ./internal/runtime -run '^Test(PiAssistantResumesSessionAcrossRetry|NodeStatePreservesTruncatedForContextErrors)$' -count=1

if [[ "${TAKT_PI_SMOKE:-0}" == "1" ]]; then
  go test ./internal/assistant -run '^TestPiAdapterOptInSmoke$' -count=1
fi

echo 'Pi adapter contract suite: PASS'
