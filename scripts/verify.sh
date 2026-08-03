#!/usr/bin/env bash
set -euo pipefail

go fmt ./...
go vet ./...
go test ./...
go build -o bin/takt ./cmd/takt

./bin/takt validate examples/route-dsl/workflow.yaml \
  --config examples/route-dsl/config.yaml \
  --workspace examples/route-dsl \
  --json >/dev/null

./bin/takt validate examples/hook-retry/workflow.yaml \
  --config examples/hook-retry/config.yaml \
  --workspace examples/hook-retry \
  --json >/dev/null

echo 'verification: PASS'
