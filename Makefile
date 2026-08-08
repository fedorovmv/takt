GO_TEST_P ?= 8

.PHONY: build test race vet fmt docs manifest contracts adapter-platform-contract package-distribution-contract multi-repo-contract runtime-reliability-contract iteration-history-contract compatibility-contract reference-adapters-contract task-source-contract learning-loop-contract architecture-contract schema-contract agent-adapter-conformance pi-contracts opencode-contracts route-e2e route-eval route-benchmark route-strategy-benchmark-contract task-evaluation-contract composition skill profile worktree-contract child-run-contract policy-contract fanout-contract script-artifact-contract mcp-contract external-executor-contract deep-workflow-contract authoring-contract daemon-contract autonomous-run-contract host-control-contract host-integration-typescript simple-reliable-contract evidence-routing-contract e2e smoke check demo

build:
	go build -o bin/takt ./cmd/takt

test:
	go test -p $(GO_TEST_P) ./... -count=1

race:
	go test -race -p $(GO_TEST_P) ./... -count=1

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal sdk reference tests

docs:
	./scripts/check-docs.sh

manifest:
	./scripts/verify-manifest.sh

# Go-native contract suites. These aliases are kept for developer muscle memory,
# but the canonical full contract command is `go test ./...`.
contracts:
	go test ./internal/assistant -count=1

pi-contracts:
	go test ./internal/assistant -run 'Pi|VersionProbe' -count=1

opencode-contracts:
	go test ./internal/assistant -run 'OpenCode|VersionProbe' -count=1

route-e2e:
	go test ./tests/e2e -run '^TestRouteDSLE2EContract$$' -count=1

route-eval:
	go test ./tests/e2e -run '^TestRouteDSLEvaluationContract$$' -count=1

route-benchmark route-strategy-benchmark-contract:
	go test ./tests/e2e -run '^TestRouteDSLBenchmarkContract$$' -count=1

task-evaluation-contract:
	go test ./internal/evaluation ./internal/application -count=1

composition:
	go test ./internal/definition ./internal/runtime ./tests/e2e -run 'Composition' -count=1

skill:
	go test ./tests/e2e -run '^TestTaktSkillContract$$' -count=1

profile:
	go test ./internal/profile ./tests/e2e -run 'Profile|CodeProfile' -count=1

worktree-contract:
	go test ./internal/gitworktree ./internal/runtime ./tests/e2e -run 'Worktree' -count=1

child-run-contract:
	go test ./internal/runtime -run 'Child' -count=1

policy-contract:
	go test ./internal/runtime -run 'Policy|Capability' -count=1

fanout-contract:
	go test ./internal/runtime -run 'Fanout' -count=1

script-artifact-contract:
	go test ./internal/runtime -run 'Script|Artifact' -count=1

mcp-contract:
	go test ./internal/mcp ./tests/e2e -run 'MCP' -count=1

external-executor-contract:
	go test ./internal/application ./internal/runtime -run 'External|Tool|Pending|Claim|Reconcile' -count=1

authoring-contract:
	go test ./internal/authoring ./internal/runtime ./tests/e2e -run 'Authoring' -count=1

daemon-contract:
	go test ./internal/daemon ./tests/e2e -run 'Daemon' -count=1

autonomous-run-contract:
	go test ./internal/application ./internal/daemon -run 'Pause|Resume|Retry|Abandon|Attention|Notification' -count=1

simple-reliable-contract:
	go test ./internal/taskroute ./internal/application ./tests/e2e -run 'SimpleReliable|Router' -count=1

evidence-routing-contract:
	go test ./internal/evidence ./internal/application ./internal/runtime -run 'Evidence|Baseline|Parking|Repair' -count=1

adapter-platform-contract:
	go test ./internal/domainadapter ./sdk/domainadapter ./tests/e2e -run 'Adapter|Domain' -count=1

multi-repo-contract:
	go test ./internal/workspacecatalog ./internal/runtime ./internal/application -run 'Repository|Workspace|Multi' -count=1

runtime-reliability-contract:
	go test ./internal/runtime ./internal/store ./internal/localsandbox ./internal/redact -count=1

iteration-history-contract:
	go test ./internal/runtime ./internal/store -run 'Iteration|Loop|Resume' -count=1

compatibility-contract:
	go test ./internal/compatibility ./tests/e2e -run 'Compatibility' -count=1

task-source-contract:
	go test ./internal/tasksource ./sdk/tasksource ./reference/githubtask ./tests/e2e -run 'TaskSource|TaskSources' -count=1

learning-loop-contract:
	go test ./internal/learning ./tests/e2e -run 'Learning' -count=1

architecture-contract:
	go test ./internal/architecture -count=1

schema-contract:
	go test ./internal/schemacontract ./internal/schemasubset ./internal/config ./internal/compatibility -run 'Schema|Subset|Meta' -count=1

agent-adapter-conformance:
	go test ./sdk/agentadapter -count=1

# A deliberately small set of shell smoke tests remains where the boundary
# being tested is process/language/package integration rather than Go logic.
package-distribution-contract: build
	./scripts/test-package-distribution.sh

reference-adapters-contract: build
	./scripts/test-reference-adapters.sh

deep-workflow-contract: build
	go build -o bin/takt-fake-code-agent ./cmd/takt-fake-code-agent
	./scripts/test-deep-code-workflows.sh

host-control-contract: build
	./scripts/test-host-control.sh

host-integration-typescript:
	./scripts/test-host-integrations-typescript.sh

e2e:
	go test ./tests/e2e -count=1

smoke: build package-distribution-contract reference-adapters-contract deep-workflow-contract host-control-contract host-integration-typescript

check: fmt vet test race build smoke docs manifest

demo: build
	./bin/takt validate examples/route-dsl/workflow.yaml --config examples/route-dsl/config.yaml
	./bin/takt run examples/route-dsl/workflow.yaml --config examples/route-dsl/config.yaml --workspace examples/route-dsl --input examples/route-dsl/specification.md
