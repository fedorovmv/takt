# Takt v0.1.54-alpha — test results

`v0.1.54-alpha` is an architecture-hardening release. Product features and public Workflow/Config/Run/MCP/adapter contracts were frozen while the remaining application/runtime/test coupling found by the post-`v0.1.53` review was removed. Verification was performed on Linux; macOS-specific regression tests remain in the suite, but this report does not claim a fresh macOS execution.

## Architecture changes verified

- shared `application.Context` removed;
- application service dependency fields are private;
- `RunService ↔ PlanService` cycle removed; fork coordination lives in `ForkService`;
- AST architecture gate rejects cycles between application services;
- concrete Dynamic Plan/Host/notification/learning/package/adapter infrastructure is wired by bootstrap rather than application use cases;
- production runtime has no default/hidden dependency constructor; evaluation receives its execution factory from bootstrap;
- CLI root context is signal-aware and foreground operations propagate caller context;
- durable detached execution is explicit; production application contains one centralized request-independent `context.Background()` helper only;
- process-global Dynamic Plan/Host mutexes removed in favor of durable store locks;
- Task response, Dynamic Plan advance and governed fan-out were decomposed into explicit phases;
- daemon/MCP canonical operation identity is shared through `internal/appapi`;
- shell `test-*.sh` surface is one TypeScript compiler smoke; process/package/host/deep-workflow assertions are Go E2E with bounded subprocess contexts.

## Working tree verification

| Check | Result |
| --- | --- |
| `gofmt` | PASS |
| `go vet ./...` | PASS |
| `go test ./... -count=1` | PASS |
| `go build ./...` | PASS |
| `go test ./internal/architecture -count=1` | PASS |
| `./scripts/check-docs.sh` | PASS |
| `make smoke` | PASS — TypeScript compiler boundary |
| release CLI validation examples | PASS |
| `go test -race -p 8 ./... -count=1` | external 5-minute limit stopped the aggregate after all printed internal/reference/SDK packages passed; no race/test failure was emitted |
| `go test -race -p 4 ./tests/e2e -count=1` | PASS |

The interrupted aggregate race run is not reported as an aggregate PASS. The only unprinted package at the cutoff was `tests/e2e`; it was run separately under `-race` and passed. This preserves the distinction between observed test success and the execution environment's wall-time limit.

## Regression coverage retained

The normal Go suite includes the previously separate contracts for:

- runtime/composition/iteration history/child Runs/fan-out;
- Dynamic Takt, Task Router and task-level evaluation;
- MCP, daemon, external executor and host control;
- Structured Task Sources and reference adapters;
- portable package distribution and multi-repo flows;
- Human-reviewed Learning Loop;
- schema subset/registry/compatibility contracts;
- Route DSL evaluation and benchmark fixtures;
- security/redaction/worktree/recovery behavior.

## Clean archive verification

A candidate ZIP was extracted into a fresh directory and verified independently of the working tree:

| Check | Result |
| --- | --- |
| `VERSION` | `0.1.54-alpha` |
| Takt skill | `0.36.0` |
| `bin/` in archive | absent |
| `MANIFEST.sha256` | PASS — 608 files |
| `./scripts/check-docs.sh` | PASS |
| `go vet ./...` | PASS |
| `go test ./... -count=1` | PASS |
| `go build ./...` | PASS |
| `go test ./internal/architecture -count=1` | PASS |
| `make smoke` | PASS |
| changed architecture/runtime packages under `-race` | PASS for application/runtime/CLI/daemon/MCP/appapi/architecture |
| clean-archive `go test -race ./tests/e2e -count=1` | PASS |

The grouped clean-archive race command was also stopped by the outer five-minute execution limit after all printed architecture/runtime packages had passed and before `tests/e2e` printed a result. `tests/e2e` was then executed separately under `-race` and passed. No interrupted aggregate is reported as PASS.
