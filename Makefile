.PHONY: build test race vet fmt docs contracts pi-contracts opencode-contracts route-e2e route-eval route-benchmark composition skill profile worktree-contract child-run-contract policy-contract fanout-contract script-artifact-contract mcp-contract external-executor-contract deep-workflow-contract authoring-contract daemon-contract check demo

build:
	go build -o bin/takt ./cmd/takt

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

docs:
	./scripts/check-docs.sh

contracts:
	./scripts/test-fake-assistant.sh

pi-contracts:
	./scripts/test-pi-adapter.sh

opencode-contracts:
	./scripts/test-opencode-adapter.sh

route-e2e: build
	./scripts/test-route-dsl-e2e.sh

route-eval: build
	./scripts/test-route-dsl-eval.sh

route-benchmark: build
	./examples/route-dsl-benchmark/run.sh

composition: build
	./scripts/test-composition.sh

skill: build
	./scripts/test-takt-skill.sh

profile: build
	./scripts/test-code-profile.sh

worktree-contract: build
	./scripts/test-worktree.sh

child-run-contract: build
	./scripts/test-child-runs.sh

policy-contract: build
	go build -o bin/takt-fake-assistant ./cmd/takt-fake-assistant
	./scripts/test-policies.sh

fanout-contract: build
	./scripts/test-child-fanout.sh

script-artifact-contract: build
	./scripts/test-script-artifacts.sh

mcp-contract: build
	./scripts/test-mcp.sh

external-executor-contract: build
	./scripts/test-external-executor.sh

deep-workflow-contract: build
	go build -o bin/takt-fake-code-agent ./cmd/takt-fake-code-agent
	./scripts/test-deep-code-workflows.sh

authoring-contract: build
	./scripts/test-authoring.sh

daemon-contract: build
	./scripts/test-daemon.sh

dynamic-takt-contract: build
	./scripts/test-dynamic-takt.sh

block-package-contract: build
	./scripts/test-block-packages.sh

check: fmt vet test race build contracts pi-contracts opencode-contracts route-e2e route-eval composition skill profile worktree-contract child-run-contract policy-contract fanout-contract script-artifact-contract mcp-contract external-executor-contract deep-workflow-contract authoring-contract daemon-contract dynamic-takt-contract block-package-contract docs

demo: build
	./bin/takt validate examples/route-dsl/workflow.yaml --config examples/route-dsl/config.yaml
	./bin/takt run examples/route-dsl/workflow.yaml --config examples/route-dsl/config.yaml --workspace examples/route-dsl --input examples/route-dsl/specification.md
