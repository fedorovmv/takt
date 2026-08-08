# Takt v0.1.53-alpha — test results

`v0.1.53-alpha` is a test-architecture-only release. Product features and public runtime contracts were frozen while the historical shell/Python contract layer was moved into standard Go tests and a shared black-box Go E2E harness. Verification was performed on Linux. macOS-specific regressions remain in the suite; this report does not claim a fresh macOS run.

## Working tree

PASS:

- `gofmt` over `cmd`, `internal`, `sdk`, `reference`, `tests`;
- `go vet ./...`;
- `go test ./... -count=1`;
- `go build ./...`;
- uninterrupted `go test -race ./... -count=1`;
- `go test ./internal/architecture ./internal/schemacontract ./tests/e2e -count=1`;
- `scripts/check-docs.sh`;
- all five retained external smoke boundaries via `make smoke`.
- release orchestration uses bounded package parallelism (`GO_TEST_P=8` by default) to avoid process-heavy suites contending unpredictably.

The five shell smoke tests are intentionally limited to package/process/language boundaries:

- portable package distribution;
- reference external adapters;
- one representative deep code workflow through Git + fake `gh` + process coding agent;
- host control;
- TypeScript host integration.

Product correctness is no longer distributed across those scripts. The full code-profile catalog, runtime/application semantics, schemas, CLI/daemon/MCP contracts, evaluation, learning, Dynamic Takt, Task Sources and compatibility are owned by Go tests.

## Go-native test architecture

The release replaces the previous 38 `scripts/test-*.sh` suites (about 3250 lines) with three explicit levels:

1. package-level `*_test.go` for unit/component contracts;
2. `tests/e2e` for black-box process-level product tests;
3. five bounded shell smoke tests where the external environment itself is part of the contract.

`tests/e2e` builds real test binaries once per test process and provides reusable helpers for temporary projects, command execution, JSON/JSONL/JSON-RPC decoding, filesystem assertions, Git fixtures and eventual conditions. Tests do not delegate product assertions to shell or embedded Python.

The schema registry/offline contract moved from a Python mini-validator to `internal/schemacontract`. It verifies Draft 2020-12 declarations, registry documentation and local-only `$ref` usage directly in Go. Test-only `routee2eassert` and `evalassert` binaries were removed.

`internal/architecture.TestShellSmokeBudget` contains the exact five-file allowlist. Adding another `scripts/test-*.sh` without explicitly changing the architectural decision fails the normal Go suite.

## Compatibility retained

Historical Make target names remain available for developer muscle memory, but target implementations now dispatch to Go packages/tests whenever the checked semantics are inside the Go module. `make e2e` runs black-box Go tests; `make smoke` runs only the five external smoke boundaries.

The former monolithic `test-deep-code-workflows.sh` was reduced from an exhaustive multi-workflow regression suite to one representative external smoke. The complete 19-workflow code profile is validated by Go E2E, while shared execution/recovery/policy behavior remains covered by runtime/application tests.

No workflow/config apiVersion, durable Run/event format, MCP tool name, daemon protocol revision, adapter protocol, evaluation schema or learning schema was intentionally changed by this release.

## Architecture decisions

ADR-086 records the new test boundary: Go owns product correctness; shell is a bounded external smoke layer. ADR-085 was updated only to point at the current Go-native architecture gate (`go test ./internal/architecture`) after removal of its historical wrapper script.

Detailed rationale and migration notes are in `docs/67-go-native-test-architecture-v0.1.53.md`.

## Composite release command

The individual ordinary and uninterrupted race suites both passed on the final working tree. A composite `make check` was also attempted after those passes; the execution environment's five-minute command ceiling terminated the process while the race toolchain was being rebuilt after the ordinary suite. No test failure was reported. This report therefore relies on the completed constituent gates rather than claiming that one composite invocation finished inside that external wall-clock limit.

## Clean release archive

The release-candidate ZIP was extracted into a fresh directory before final packaging. PASS from that clean extraction:

- `VERSION` = `0.1.53-alpha`; Takt skill = `0.35.0`;
- `bin/` absent before verification;
- manifest verification: 603 files;
- documentation gate;
- `go vet ./...`;
- `go test -p 8 ./... -count=1`;
- `go build ./...`;
- all five external smoke boundaries via `make smoke`;
- `internal/architecture`, `internal/schemacontract` and the full black-box `tests/e2e` contour under `-race`.

The working tree had already passed one uninterrupted full `go test -race ./... -count=1`; the clean-archive race rerun intentionally focused on the packages introduced or materially changed by this test-architecture release. Verification-generated `bin/` remains excluded from the archive and manifest.
