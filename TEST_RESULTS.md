# Takt v0.1.56-alpha — TEST RESULTS

Release theme: **Codebase Hygiene & Stabilization**. No product capability was intentionally removed and no new Workflow/Run/SDK operation was added.

## Codebase / architecture

PASS:

- stable `internal/application` no longer owns the external worker/tool subsystem; it is isolated in `internal/externalworker`;
- common durable lock/redaction/reload helpers are centralized in `internal/runcontrol` rather than copied between Run and external-worker lifecycles;
- `internal/application` is about 2.2k production LOC (down from ~3.5k in v0.1.55 and ~6.8k before modularization);
- stable `internal/assistant` is provider-neutral; Pi/OpenCode implementations are under `internal/extensions/assistants` and are injected by `internal/bootstrap`;
- fake assistant/code/domain/Pi/OpenCode binaries live under `internal/testsupport/cmd`, not product `cmd/`;
- stable core does not import `internal/experimental` or `internal/tooling`, and stable assistant/application/external-worker code does not import provider extension implementations;
- Dynamic Flow / Host Control / Learning remain available under the experimental boundary;
- evaluation / compatibility remain available under tooling;
- architecture tests reject the return of fake product commands, provider-specific assistant code in core, the old JSON Schema engine, service dependency cycles and reverse stable-module imports.

Production LOC after the refactor (tests and `internal/testsupport` excluded where applicable):

| Area | LOC |
| --- | ---: |
| `internal/application` | 2,159 |
| `internal/externalworker` | 1,279 |
| `internal/runcontrol` | 86 |
| stable `internal/assistant` | 1,368 |
| Pi extension | 941 |
| OpenCode extension | 761 |
| `internal/schemasubset` | 213 |
| `internal/runtime` | 5,226 |
| `internal/experimental` | 5,639 |
| `internal/tooling` | 3,185 |

The repository still contains the existing experimental/tooling/extension functionality; the LOC reduction in stable application/core comes from moving independent responsibilities, not deleting features.

## Commodity libraries

PASS at the code-contract level:

- YAML syntax is delegated to `go.yaml.in/yaml/v3 v3.0.4`; Takt keeps only its strict `yamlcodec` contract/diagnostics;
- JSON Schema Draft 2020-12 instance validation is delegated to `github.com/santhosh-tekuri/jsonschema/v6 v6.0.2`;
- `internal/schemasubset` now owns only the Takt-specific allowed-subset policy and canonical JSON normalization;
- compatibility/schema tests use the same upstream JSON Schema API; the second test-only validator was removed;
- `go.mod`/`go.sum` contain normal pinned dependencies and contain no `replace` or sandbox path.

Sandbox limitation: this execution environment has no normal external module DNS/cache for the newly added YAML/JSON Schema modules. Verification here therefore used temporary API-compatible modules **outside the release tree** through `-modfile`. No replacement is present in the release. A networked clean Go/CI build against the exact upstream module contents remains required before promotion beyond alpha.

## Stable runtime / assistant decomposition

PASS:

- `takt-assistant/v1alpha2` process execution is split into process startup, stream read/decode, record dispatch, tool/result handling and final process verification;
- script execution is split into working-directory, argument rendering, policy/sandbox, command configuration and result mapping;
- governed child Run execution is split into definition/output preparation, durable lineage, start/resume, isolation options and terminal projection;
- logical assistant configuration validation is independent of provider implementation availability;
- runtime capability preflight and actual execution use the same injected assistant resolver;
- evaluation execution uses the same bundled assistant provider set as normal runtime wiring (regression caught by black-box Route DSL evaluation).

## User / black-box journeys

PASS using the real `takt` binary through the Go E2E harness:

1. `init -> validate -> run -> status/events/artifacts`;
2. approval -> `answer` -> continue;
3. failed Run -> `retry` -> completed;
4. reusable subworkflow;
5. MCP contract;
6. daemon lifecycle;
7. Route DSL E2E/evaluation/strategy benchmark;
8. Dynamic Takt, Task Sources, Host Control and Simple Reliable Router experimental contracts;
9. package distribution/reference adapters/adapter platform boundaries;
10. authoring, composition, worktree, learning and profile catalog contracts.

The E2E harness has a test-only `TAKT_TEST_GO_MODFILE` build hook so sandbox dependency substitution is used only when compiling Takt fixtures and is not leaked into workflows/subprocesses under test.

## Test results

PASS on the final working tree:

- `gofmt` on Go sources;
- `go vet ./...` (with sandbox-only external modfile);
- `go build ./...` (same condition);
- all non-runtime Go packages, executed in bounded groups;
- all runtime tests, executed in two bounded groups covering the complete `Test*` set;
- all Go E2E contracts, executed in bounded logical groups;
- `./scripts/check-docs.sh`;
- `./scripts/test-host-integrations-typescript.sh`.

Race PASS for the changed/stable contour:

- `internal/application`, `internal/externalworker`, `internal/runcontrol`;
- `internal/assistant`, Pi/OpenCode provider extensions;
- `internal/schemasubset`, compatibility, workflow and architecture;
- CLI, daemon and MCP;
- governed child Run, structured-output/schema/script and assistant-provider runtime regressions;
- user journeys, MCP/daemon E2E and Route DSL evaluation/benchmark E2E.

A single uninterrupted `go test -race ./...` is not claimed for this release because the container command channel has a hard execution limit and process-heavy Go suites exceed it. The changed surfaces above were run under race in bounded groups.

## Release-specific regression found and fixed

The black-box evaluation suite found that, after moving Pi/OpenCode out of stable `internal/assistant`, the dedicated evaluation execution factory was still constructing an assistant factory without provider extensions. Normal runs worked, while evaluation reported `unknown assistant: pi`. `internal/bootstrap/evaluation.go` now injects the same provider factories used by the primary runtime composition root; Route DSL evaluation and benchmark contracts pass again.

## Release promotion note

The architecture/codebase cleanup is considered complete after this release. Further stabilization should be driven by actual installation and live user/provider scenarios. Promotion beyond alpha additionally requires one networked clean build/test using the exact pinned upstream YAML and JSON Schema modules rather than the sandbox compatibility substitutes used here.

## Clean archive verification

The release candidate was unpacked into a new directory and checked independently of the working tree:

- `VERSION=0.1.56-alpha`, Takt skill `0.38.0`;
- `bin/` absent;
- manifest PASS with 621 tracked files;
- documentation gate PASS;
- no sandbox path or local `replace` in the release tree;
- `go vet ./...` and `go build ./...` PASS with the sandbox-only external modfile described above;
- stable application/assistant/provider/schema/tooling/workflow/architecture/MCP/daemon/CLI tests PASS;
- the complete runtime `Test*` set PASS in two bounded groups;
- user journeys + MCP/daemon + Route DSL evaluation/benchmark black-box E2E PASS;
- TypeScript host integration smoke PASS.
