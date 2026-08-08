# Takt v0.1.52-alpha — test results

`v0.1.52-alpha` is an architecture-only release. Product features were frozen while the CLI/control/runtime composition was refactored around an explicit application boundary. Verification was performed on Linux. macOS-specific regressions remain in the suite; this report does not claim a fresh macOS run.

## Working tree

PASS:

- `gofmt` over `cmd`, `internal`, `sdk`, `reference`;
- `go vet ./...`;
- `go test ./... -count=1`;
- `go build ./...`;
- `scripts/test-architecture.sh`;
- `scripts/check-docs.sh`;
- schema registry contract: 36 JSON Schemas;
- all repository `scripts/test-*.sh` contracts: 38/38 PASS;
- changed architecture contour under `-race`: application, appapi, bootstrap, CLI, MCP, daemon, runtime, workflow, config, learning, evaluation and architecture;
- remaining tested internal/reference/SDK packages under `-race` in bounded groups.

The 38 contract scripts cover authoring, compatibility/schema contracts, BlockPackage/package distribution, Adapter Platform/reference adapters, Structured Task Sources, learning loop, composition/child runs/fan-out, daemon/MCP, autonomous runs, Dynamic Takt, host control, external executor, iteration history, runtime/security, worktree/multi-repo, policy/artifacts, evidence routing, simple-reliable routing, fake/Pi/OpenCode adapters, Takt skill/code profile, deep workflows, Route DSL evaluation/E2E/benchmark, task evaluation and TypeScript host integrations.

A single `make check` completed `gofmt`, `go vet` and full ordinary tests, then entered `go test -race ./...`. The external five-minute command limit terminated that aggregate race run after successful packages through `internal/architecture`; no race failure was observed. All packages with tests were then exercised under `-race` in bounded groups and passed. This report therefore does not claim one uninterrupted aggregate `go test -race ./...` PASS.

## Architecture regression coverage

The new architecture gate verifies that:

- legacy `internal/control` stays removed;
- production `cmd/takt` remains a thin launcher and is capped by an import/line boundary;
- production CLI cannot import runtime/store/evaluation/learning/package engines directly;
- application cannot depend on transports, appapi or bootstrap;
- appapi cannot depend on runtime/transports;
- MCP/daemon cannot bypass application into runtime/evaluation/notification;
- production application does not construct concrete `store.FS`;
- all dependency/state fields of `runtime.Runner` remain private.

Additional regression tests cover the legacy CLI contract where an explicitly supplied workflow/config path is relative to the caller's current directory even when `--workspace` points elsewhere. This regression was found by the Takt skill E2E after the CLI migration and is now fixed without moving workflow semantics back into the transport layer.

## Refactor compatibility fixes found by E2E

The architecture migration also exposed release-test coupling to removed source locations. Dynamic Takt, evidence routing, simple-reliable routing and runtime/security scripts were updated from `internal/control` to `internal/application`, and the Dynamic Takt source assertion now checks `internal/cli` rather than `cmd/takt/main.go`.

No public workflow/config apiVersion, durable Run/event format, MCP tool name, daemon protocol revision, adapter protocol or evaluation/learning schema was intentionally changed by this release.

## Clean release archive

A release-candidate ZIP was extracted into a new directory before final packaging. PASS from that clean extraction:

- `VERSION` = `0.1.52-alpha`; Takt skill = `0.34.0`;
- `bin/` absent before verification;
- manifest verification: 631 files;
- architecture and documentation gates;
- `go vet ./...`, `go test ./... -count=1`, `go build ./...`;
- daemon, MCP, Dynamic Takt, Structured Task Sources, learning loop, package distribution, reference adapters, iteration history, runtime/security, Takt skill, Route DSL benchmark and task-level evaluation contracts;
- changed architecture contour under `-race`, split into bounded commands.

The long clean-extraction E2E group itself reached the environment command limit only while entering the final task-evaluation script; the preceding contracts had passed. `test-task-evaluation.sh` was then run separately and passed. The same bounded approach was used for the final runtime/workflow/config/learning/evaluation architecture race group.

Verification scripts generated `bin/` only after the initial package-hygiene check. `bin/` remains excluded from the release archive and manifest by design.
