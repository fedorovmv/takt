.PHONY: build test race vet fmt docs contracts pi-contracts route-e2e route-eval route-benchmark skill check demo

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

route-e2e: build
	./scripts/test-route-dsl-e2e.sh

route-eval: build
	./scripts/test-route-dsl-eval.sh

route-benchmark: build
	./examples/route-dsl-benchmark/run.sh

skill: build
	./scripts/test-takt-skill.sh

check: fmt vet test race build contracts pi-contracts route-e2e route-eval skill docs

demo: build
	./bin/takt validate examples/route-dsl/workflow.yaml --config examples/route-dsl/config.yaml
	./bin/takt run examples/route-dsl/workflow.yaml --config examples/route-dsl/config.yaml --workspace examples/route-dsl --input examples/route-dsl/specification.md
