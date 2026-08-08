#!/usr/bin/env bash
set -euo pipefail

gofmt -w cmd internal sdk
go vet ./...
go test ./...
go test -race ./...
go build -o bin/takt ./cmd/takt
go build -o bin/takt-fake-assistant ./cmd/takt-fake-assistant
go build -o bin/takt-fake-pi ./cmd/takt-fake-pi
go build -o bin/takt-fake-opencode ./cmd/takt-fake-opencode
go build -o bin/takt-fake-code-agent ./cmd/takt-fake-code-agent
go build -o bin/takt-fake-domain-adapter ./cmd/takt-fake-domain-adapter
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
./scripts/test-child-fanout.sh
./scripts/test-script-artifacts.sh
./scripts/test-mcp.sh
./scripts/test-external-executor.sh
./scripts/test-deep-code-workflows.sh
./scripts/test-authoring.sh
./scripts/test-daemon.sh
./scripts/test-dynamic-takt.sh
./scripts/test-block-packages.sh
./scripts/test-host-control.sh
./scripts/test-host-integrations-typescript.sh
./scripts/test-autonomous-runs.sh
./scripts/test-simple-reliable-router.sh
./scripts/test-evidence-routing.sh
./scripts/test-adapter-platform.sh
./scripts/test-package-distribution.sh
./scripts/test-multi-repo.sh
./scripts/test-runtime-reliability-security.sh
./scripts/test-iteration-history.sh
./scripts/test-compatibility.sh
./scripts/test-route-dsl-benchmark.sh
./scripts/test-task-evaluation.sh
go test ./sdk/agentadapter -count=1
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
