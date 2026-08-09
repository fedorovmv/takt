# Takt v0.1.57-alpha — TEST RESULTS

## Post-audit repair — 2026-08-10

PASS на ветке `fix/release-gate-validation`:

- `go mod tidy` восстановил обязательные indirect module/checksum records, после чего `go vet ./...` завершился успешно;
- новые `yamlcodec` regressions сначала воспроизвели принятие второго JSON/YAML document и invalid trailing JSON, затем прошли после single-document fix;
- новые `whenexpr` regressions сначала воспроизвели принятие unterminated/mismatched quoted literals, затем прошли после delimiter validation;
- `go test ./... -count=1` прошёл для всех пакетов и полного `tests/e2e`;
- `go test -race ./... -count=1` прошёл непрерывным aggregate запуском;
- required TypeScript smoke прошёл с изолированно установленным `typescript@5.7.2`; CI устанавливает ту же pinned версию и выставляет `TAKT_REQUIRE_TYPESCRIPT=1`;
- `make check` прошёл с обязательным TypeScript smoke, documentation и manifest gates;
- `./scripts/verify.sh` прошёл полностью и завершился `verification: PASS`.

Release theme: **Architecture Contracts**. Product capabilities and the external `takt/v1alpha1` Workflow/Config API were intentionally not expanded.

## Architecture contracts

PASS:

### Workflow Language Constitution

- normative rule is documented in `docs/04-architecture.md`, `docs/03-specification.md`, `AGENTS.md`, ADR-090 and `docs/72-architecture-contracts-v0.1.57.md`;
- `internal/whenexpr` is the single implementation of `when` parsing/evaluation;
- supported gate language remains only `==`, `!=`, `&&`, `||`, `nodes.*`/`inputs.*` paths and literals;
- parentheses/functions/arithmetic/order operators are rejected;
- `internal/workflow` validates `when` before Run creation and `internal/runtime` uses the same implementation;
- workflow regression tests cover accepted gates, hyphenated node IDs and rejected expression creep.

### Immutable provider registrations

- bundled Pi/OpenCode extension packages declare `assistant.ProviderRegistration` values;
- production provider registry is assembled exactly once in `internal/bootstrap`;
- no global mutable provider registry or `init()` registration path exists;
- `assistant.Registry` copies registrations, rejects duplicate IDs and returns deterministic read-only snapshots;
- runtime and tooling receive the same provider registry graph from bootstrap;
- architecture tests reject production registry construction outside bootstrap/testsupport.

### Schema-first canonical operations

- `internal/appapi.OperationDescriptor` owns operation ID, stability stage, MCP name, title, description, JSON InputSchema and annotations;
- `registerOperation[T]` binds each descriptor to a concrete Go request type;
- descriptor schema validates input before strict typed decode;
- top-level JSON fields of each Go request are checked against descriptor schema during registration and by contract tests;
- duplicate operation IDs/MCP names fail fast;
- descriptor snapshots deep-copy schema/annotation maps and cannot mutate canonical state;
- MCP canonical tools are projected from `appapi.CanonicalOperations()` and a test checks exact metadata/schema equality;
- `docs/71-canonical-operation-contracts.generated.md` is generated from the same descriptors and a regression test rejects documentation drift.

A pre-existing contract mismatch was exposed during implementation: `host.begin` accepted `candidate` in the typed Go request but the MCP schema did not publish it. The descriptor was corrected; the new typed-schema alignment check prevents that class of drift even when a particular E2E payload is not exercised.

## Verification

PASS on the final working tree after the architecture-contract changes:

- `gofmt` for changed Go sources;
- `go vet ./...` using the sandbox-only external modfile described below;
- `go build ./...` under the same condition;
- ordinary tests for the complete repository in bounded verification: the broad suite passed all packages except the known sandbox `GOFLAGS` collision in the runtime Go-script subtest, and that complete runtime package then passed with the test binary launched without the sandbox-only `GOFLAGS`;
- complete `tests/e2e` suite;
- stable user journeys (`^TestUserJourney`);
- `internal/architecture`;
- `internal/assistant`, bundled provider extensions and tooling compatibility/evaluation;
- `internal/whenexpr`, `internal/workflow`, `internal/runtime`, `internal/appapi`, `internal/mcp`;
- `./scripts/check-docs.sh`;
- `./scripts/test-host-integrations-typescript.sh`.

Race PASS in bounded groups for the changed contour:

- `internal/assistant` and Pi/OpenCode extensions;
- tooling compatibility/evaluation;
- `internal/whenexpr` and `internal/workflow`;
- `internal/runtime`, `internal/appapi`, `internal/mcp`, `internal/architecture`;
- complete `tests/e2e`.

The first combined race command was stopped by the outer command-channel time limit after the assistant/providers/tooling/when/workflow packages had already printed PASS. The remaining runtime/appapi/MCP/architecture packages and E2E were then run separately under `-race` and passed. An uninterrupted aggregate `go test -race ./...` is therefore not claimed.

## Sandbox module limitation

This execution environment has no normal external module DNS/cache for the pinned YAML/JSON Schema dependencies introduced in prior releases. Verification therefore compiles Takt with a temporary API-compatible `-modfile` located **outside the release tree**. For tests that intentionally execute a user's `go run` script, the compiled test binary is launched with that temporary `GOFLAGS` removed; this prevents a Takt-specific modfile from leaking into the user's Go process.

No `replace`, shim path or `/mnt/data` path is part of the project tree or release archive. A networked CI run against the exact pinned upstream modules remains required before promotion beyond alpha, as already noted for v0.1.55/v0.1.56.

## Release contract

Expected release metadata:

- Takt `0.1.57-alpha`;
- authoring skill `0.39.0`;
- Workflow/Config API remains `takt/v1alpha1`;
- ADR-090 present in `ARCHITECTURE_DECISIONS.md`;
- normative architecture contract present in `docs/04-architecture.md` and `docs/72-architecture-contracts-v0.1.57.md`;
- generated canonical operation surface present in `docs/71-canonical-operation-contracts.generated.md`.

## Clean archive verification

The candidate ZIP was extracted into a new directory and checked independently of the working tree:

- `VERSION=0.1.57-alpha`, authoring skill `0.39.0`;
- `bin/` absent before verification;
- manifest PASS with 628 tracked files;
- documentation gate PASS;
- no `/mnt/data` sandbox path or local dependency `replace` in the release tree;
- `go vet ./...` and `go build ./...` PASS under the sandbox-only external modfile described above;
- assistant registry, `whenexpr`, workflow validation, appapi, MCP and architecture contracts PASS;
- the complete `internal/runtime` package PASS with the test binary isolated from sandbox `GOFLAGS`;
- complete `tests/e2e` PASS;
- TypeScript host integration smoke PASS.

The first combined clean-archive test command was stopped by the outer command channel after runtime had already printed PASS; E2E and the TypeScript smoke were then run separately and passed.
