.PHONY: build test race vet fmt docs contracts pi-contracts route-e2e check demo

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

check: fmt vet test race build contracts pi-contracts route-e2e docs

demo: build
	./bin/takt validate examples/route-dsl/workflow.yaml --config examples/route-dsl/config.yaml
	./bin/takt run examples/route-dsl/workflow.yaml --config examples/route-dsl/config.yaml --workspace examples/route-dsl --input examples/route-dsl/specification.md
