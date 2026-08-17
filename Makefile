GO_TEST_P ?= 8
PI_SMOKE_PROVIDER ?= aihub
PI_SMOKE_MODEL ?= Qwen/Qwen3.6-27B
EVAL_IDLE_TIMEOUT ?= 5m
EVAL_PRESET ?=
EVAL_MODEL_FLAGS ?=

.PHONY: build test race vet fmt contracts adapter-platform-contract package-distribution-contract multi-repo-contract runtime-reliability-contract iteration-history-contract compatibility-contract reference-adapters-contract task-source-contract learning-loop-contract architecture-contract schema-contract agent-adapter-conformance pi-contracts opencode-contracts route-e2e route-eval route-benchmark route-strategy-benchmark-contract task-evaluation-contract composition skill profile worktree-contract child-run-contract policy-contract fanout-contract script-artifact-contract mcp-contract external-executor-contract deep-workflow-contract authoring-contract daemon-contract autonomous-run-contract host-control-contract host-integration-typescript simple-reliable-contract evidence-routing-contract e2e journeys smoke check demo eval-smoke eval-feature-smoke eval-feature eval-review eval-architect eval-stats eval-status eval-inspect eval-compare eval-analyze

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

# Go-native contract suites. These aliases are kept for developer muscle memory,
# but the canonical full contract command is `go test ./...`.
contracts:
	go test ./internal/assistant -count=1

pi-contracts:
	go test ./internal/assistant -run 'Pi|VersionProbe' -count=1

eval-smoke:
	TAKT_PI_SMOKE=1 TAKT_PI_SMOKE_PROVIDER=$(PI_SMOKE_PROVIDER) TAKT_PI_SMOKE_MODEL=$(PI_SMOKE_MODEL) go test ./internal/extensions/assistants/pi -run '^TestPiAdapterOptInSmoke$$' -count=1 -v

eval-feature-smoke:
	@test -f examples/flow-evaluation/mini-du/config.yaml || { echo 'missing examples/flow-evaluation/mini-du/config.yaml'; exit 1; }
	@echo 'Starting live feature smoke: workflow=code:feature-development case=implement-basic flags=-s,-k,-H,-h,--help,--,-sk,-ks,-sH preset=$(EVAL_PRESET) model_flags=$(EVAL_MODEL_FLAGS) assistant_idle_timeout=$(EVAL_IDLE_TIMEOUT)'
	go run ./cmd/takt eval flow examples/flow-evaluation/mini-du/feature-development/suite.yaml --case implement-basic $(if $(EVAL_PRESET),--model-preset $(EVAL_PRESET)) $(EVAL_MODEL_FLAGS) --assistant-idle-timeout $(EVAL_IDLE_TIMEOUT) --trace --json >/dev/null

eval-feature:
	@test -f examples/flow-evaluation/mini-du/config.yaml || { echo 'missing examples/flow-evaluation/mini-du/config.yaml'; exit 1; }
	@echo 'Starting full live feature evaluation: workflow=code:feature-development cases=all flags=-s,-k,-H,-h,--help,--,-sk,-ks,-sH preset=$(EVAL_PRESET) model_flags=$(EVAL_MODEL_FLAGS) assistant_idle_timeout=$(EVAL_IDLE_TIMEOUT)'
	go run ./cmd/takt eval flow examples/flow-evaluation/mini-du/feature-development/suite.yaml $(if $(EVAL_PRESET),--model-preset $(EVAL_PRESET)) $(EVAL_MODEL_FLAGS) --assistant-idle-timeout $(EVAL_IDLE_TIMEOUT) --trace --json >/dev/null

eval-stats:
	@test -n "$(RUN)" || { echo 'usage: make eval-stats RUN=.takt/evals/...'; exit 1; }
	@go run ./cmd/takt eval stats "$(RUN)" --json=false

eval-status:
	@test -n "$(RUN)" || { echo 'usage: make eval-status RUN=.takt/evals/...'; exit 1; }
	@go run ./cmd/takt eval status "$(RUN)" --json=false

eval-inspect:
	@test -n "$(RUN)" || { echo 'usage: make eval-inspect RUN=.takt/evals/... [CASE=case-id] [REPEAT=1]'; exit 1; }
	@go run ./cmd/takt eval inspect "$(RUN)" $(if $(CASE),--case "$(CASE)") $(if $(REPEAT),--repeat "$(REPEAT)") --json=false

eval-compare:
	@test -n "$(A)" || { echo 'usage: make eval-compare A=.takt/evals/run-a B=.takt/evals/run-b'; exit 1; }
	@test -n "$(B)" || { echo 'usage: make eval-compare A=.takt/evals/run-a B=.takt/evals/run-b'; exit 1; }
	@echo 'Comparing evaluation runs: A=$(A) B=$(B) delta=B-A'
	@go run ./cmd/takt eval compare "$(A)" "$(B)" --json=false

eval-review:
	@test -f examples/flow-evaluation/mini-du/config.yaml || { echo 'missing examples/flow-evaluation/mini-du/config.yaml'; exit 1; }
	@echo 'Starting live review evaluation: workflow=code:comprehensive-pr-review case=review-hardlink-bug preset=$(EVAL_PRESET) model_flags=$(EVAL_MODEL_FLAGS) assistant_idle_timeout=$(EVAL_IDLE_TIMEOUT)'
	go run ./cmd/takt eval flow examples/flow-evaluation/mini-du/review/suite.yaml --case review-hardlink-bug $(if $(EVAL_PRESET),--model-preset $(EVAL_PRESET)) $(EVAL_MODEL_FLAGS) --assistant-idle-timeout $(EVAL_IDLE_TIMEOUT) --trace --json >/dev/null

eval-architect:
	@test -f examples/flow-evaluation/mini-du/config.yaml || { echo 'missing examples/flow-evaluation/mini-du/config.yaml'; exit 1; }
	@echo 'Starting live architect evaluation: workflow=code:architect case=collapse-redundant-layers preset=$(EVAL_PRESET) model_flags=$(EVAL_MODEL_FLAGS) assistant_idle_timeout=$(EVAL_IDLE_TIMEOUT)'
	go run ./cmd/takt eval flow examples/flow-evaluation/mini-du/architect/suite.yaml --case collapse-redundant-layers $(if $(EVAL_PRESET),--model-preset $(EVAL_PRESET)) $(EVAL_MODEL_FLAGS) --assistant-idle-timeout $(EVAL_IDLE_TIMEOUT) --trace --json >/dev/null

eval-analyze:
	@test -n "$(RUN)" || { echo 'usage: make eval-analyze RUN=.takt/evals/... [CASE=case-id] [REPEAT=1] [EVAL_CONFIG=path] [EVAL_PRESET=name] [EVAL_ANALYSIS_LANGUAGE=en|ru]'; exit 1; }
	@go run ./cmd/takt eval analyze "$(RUN)" $(if $(CASE),--case "$(CASE)") $(if $(REPEAT),--repeat "$(REPEAT)") $(if $(EVAL_CONFIG),--config "$(EVAL_CONFIG)") $(if $(EVAL_PRESET),--model-preset "$(EVAL_PRESET)") $(if $(EVAL_ANALYSIS_LANGUAGE),--language "$(EVAL_ANALYSIS_LANGUAGE)") --trace --json=false

opencode-contracts:
	go test ./internal/assistant -run 'OpenCode|VersionProbe' -count=1

route-e2e:
	go test ./tests/e2e -run '^TestRouteDSLE2EContract$$' -count=1

route-eval:
	go test ./tests/e2e -run '^TestRouteDSLEvaluationContract$$' -count=1

route-benchmark route-strategy-benchmark-contract:
	go test ./tests/e2e -run '^TestRouteDSLBenchmarkContract$$' -count=1

task-evaluation-contract:
	go test ./internal/tooling/evaluation ./internal/experimental/dynamicflow -count=1

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
	go test ./internal/experimental/taskroute ./internal/experimental/dynamicflow ./tests/e2e -run 'SimpleReliable|Router' -count=1

evidence-routing-contract:
	go test ./internal/experimental/evidence ./internal/experimental/dynamicflow ./internal/runtime -run 'Evidence|Baseline|Parking|Repair' -count=1

adapter-platform-contract:
	go test ./internal/domainadapter ./sdk/domainadapter ./tests/e2e -run 'Adapter|Domain' -count=1

multi-repo-contract:
	go test ./internal/experimental/workspacecatalog ./internal/runtime ./internal/experimental/dynamicflow -run 'Repository|Workspace|Multi' -count=1

runtime-reliability-contract:
	go test ./internal/runtime ./internal/store ./internal/localsandbox ./internal/redact -count=1

iteration-history-contract:
	go test ./internal/runtime ./internal/store -run 'Iteration|Loop|Resume' -count=1

compatibility-contract:
	go test ./internal/tooling/compatibility ./tests/e2e -run 'Compatibility' -count=1

task-source-contract:
	go test ./internal/tasksource ./sdk/tasksource ./reference/githubtask ./tests/e2e -run 'TaskSource|TaskSources' -count=1

learning-loop-contract:
	go test ./internal/experimental/learning ./tests/e2e -run 'Learning' -count=1

architecture-contract:
	go test ./internal/architecture -count=1

schema-contract:
	go test ./internal/schemacontract ./internal/schemasubset ./internal/config ./internal/tooling/compatibility -run 'Schema|Subset|Meta' -count=1

agent-adapter-conformance:
	go test ./sdk/agentadapter -count=1

# Go black-box contracts own process/package/runtime behavior. Shell is reserved
# for the one cross-language compiler smoke below.
package-distribution-contract:
	go test ./tests/e2e -run '^TestPackageDistributionBoundary$$' -count=1

reference-adapters-contract:
	go test ./tests/e2e -run '^TestReferenceAdaptersBoundary$$' -count=1

deep-workflow-contract:
	go test ./tests/e2e -run '^TestDeepCodeWorkflowBoundary$$' -count=1

host-control-contract:
	go test ./tests/e2e -run '^TestHost(ControlBoundary|IntegrationSourceContract)$$' -count=1

host-integration-typescript:
	./scripts/test-host-integrations-typescript.sh

e2e:
	go test ./tests/e2e -count=1

# Stable user-facing journeys are an explicit release gate, separate from
# internal contract coverage.
journeys:
	go test ./tests/e2e -run '^TestUserJourney' -count=1

smoke: host-integration-typescript

check: fmt vet test journeys race build smoke

demo: build
	./bin/takt validate examples/route-dsl/workflow.yaml --config examples/route-dsl/config.yaml
	./bin/takt run examples/route-dsl/workflow.yaml --config examples/route-dsl/config.yaml --workspace examples/route-dsl --input examples/route-dsl/specification.md
