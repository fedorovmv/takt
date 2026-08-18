#!/usr/bin/env bash
set -euo pipefail
GO_TEST_P="${GO_TEST_P:-8}"

# Product correctness is Go-native. Keep release verification as orchestration,
# not as a second assertion framework.
gofmt -w cmd internal sdk reference tests
go vet ./...
make GO_TEST_P="$GO_TEST_P" test-all
# User-facing stabilization gate: prove the documented stable journeys through the real CLI.
make journeys
make GO_TEST_P="$GO_TEST_P" race-all
go build -o bin/takt ./cmd/takt

# The only shell smoke is the cross-language TypeScript compiler boundary.
./scripts/test-host-integrations-typescript.sh

./bin/takt validate examples/route-dsl/workflow.yaml \
  --config examples/route-dsl/config.yaml \
  --workspace examples/route-dsl \
  --json >/dev/null
./bin/takt validate examples/hook-retry/workflow.yaml \
  --config examples/hook-retry/config.yaml \
  --workspace examples/hook-retry \
  --json >/dev/null
./bin/takt validate examples/pi-smoke/workflow.yaml \
  --config examples/pi-smoke/config.yaml \
  --workspace . \
  --json >/dev/null
./bin/takt validate examples/opencode-smoke/workflow.yaml \
  --config examples/opencode-smoke/config.yaml \
  --workspace . \
  --json >/dev/null
./bin/takt validate examples/route-dsl-e2e/workflow.yaml \
  --config examples/route-dsl-e2e/config.yaml \
  --workspace examples/route-dsl-e2e \
  --json >/dev/null
./bin/takt validate examples/composition/workflow.yaml \
  --config examples/composition/config.yaml \
  --workspace examples/composition \
  --json >/dev/null
./bin/takt validate examples/external-executor/workflow.yaml \
  --config examples/external-executor/config.yaml \
  --workspace . \
  --json >/dev/null
./bin/takt validate examples/authoring-daemon/workflow.yaml \
  --config examples/authoring-daemon/config.yaml \
  --workspace examples/authoring-daemon \
  --warnings-as-errors \
  --json >/dev/null

echo 'verification: PASS'
