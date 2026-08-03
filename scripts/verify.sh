#!/usr/bin/env bash
set -euo pipefail

gofmt -w cmd internal
go vet ./...
go test ./...
go test -race ./...
go build -o bin/takt ./cmd/takt
go build -o bin/takt-fake-assistant ./cmd/takt-fake-assistant
./scripts/test-fake-assistant.sh
./scripts/check-docs.sh

./bin/takt validate examples/route-dsl/workflow.yaml \
  --config examples/route-dsl/config.yaml \
  --workspace examples/route-dsl \
  --json >/dev/null

./bin/takt validate examples/hook-retry/workflow.yaml \
  --config examples/hook-retry/config.yaml \
  --workspace examples/hook-retry \
  --json >/dev/null

echo 'verification: PASS'
