#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

go test ./internal/assistant -run '^TestFakeAssistantContract$' -count=1
go test -race ./internal/assistant -run '^(TestFakeAssistantContract|TestProcessOutputLimitIsRaceSafeAcrossStdoutAndStderr)$' -count=1
go test ./internal/runtime -run '^TestProtocolAssistantResumesSessionAcrossRetry$' -count=1

echo 'fake-assistant contract suite: PASS'
