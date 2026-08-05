#!/usr/bin/env bash
set -euo pipefail

gofmt -w cmd internal
go vet ./...
go test ./...
go test -race ./...
go build -o bin/takt ./cmd/takt
go build -o bin/takt-fake-assistant ./cmd/takt-fake-assistant
go build -o bin/takt-fake-pi ./cmd/takt-fake-pi
go build -o bin/takt-fake-opencode ./cmd/takt-fake-opencode
./scripts/test-fake-assistant.sh
./scripts/test-pi-adapter.sh
./scripts/test-opencode-adapter.sh
./scripts/test-route-dsl-e2e.sh
./scripts/test-route-dsl-eval.sh
./scripts/test-composition.sh
./scripts/test-takt-skill.sh
./scripts/test-code-profile.sh
./scripts/test-worktree.sh
./scripts/test-child-runs.sh
./scripts/test-policies.sh
./scripts/check-docs.sh

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

echo 'verification: PASS'
