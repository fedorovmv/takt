# Takt v0.1.57-alpha — TEST RESULTS

## REQUEST CHANGES follow-up — 2026-08-12

Focused regressions cover shell comments/heredoc/nested substitutions, loop-child
schema `cancel`, explicit root `fresh_context: false`, fresh-over-shared session
precedence (including single-quoted `$(` and `case` patterns), retry session
precedence, expanded loop canonical paths and static `foreach` documentation.

## Archon-first A0/A1 contract slice — 2026-08-11

PASS (deterministic local/fake-host boundary):

- review remediation focused suite:
  `go test ./internal/flowref ./internal/authoring ./internal/redact
  ./internal/workflow ./internal/runtime ./tests/e2e -run
  'Shell|Quote|BaseBranch|InlineScript|Archon|Loop|Until|Cancel|Required|Shared|Signal|Truncat|Redact|CurrentDocumentation' -count=1`;
- `go test ./... -count=1 -timeout=300s`;
- `go test -race ./... -count=1 -timeout=300s`;
- `go vet ./...`;
- `make check` (including user journeys, race suite and TypeScript host smoke);
- `./scripts/verify.sh`;
- `go test ./tests/e2e -count=1 -timeout=300s`;
- `go test ./internal/runtime -count=1 -timeout=180s`;
- `git diff --check`.

The suite covers target Workflow/command loading, unified `$...` references,
signal matching/truncation, `until.requires`/`until_bash`, durable loop history,
approval/cancel recovery, exact session resume and Archon provenance fixtures.
No live provider budget or mutating fan-out proof is claimed; `scripts/check-docs.sh`
is intentionally absent in this branch.

## Deterministic Development Flow Acceptance — 2026-08-10

Focused PASS on `code:plan-to-pr` profile 0.17.0:

- `go test ./internal/profile -run ScopeCheck -count=1`: native Git pathspec
  matching, NUL-delimited tracked/untracked paths, rename source/destination,
  artifact report persistence and unsafe path rejection;
- `go test ./tests/e2e -run '^TestPlanToPRAcceptance$' -count=1`: one
  `safe_success` and seven `safe_stop` scenarios with zero `unsafe_success`;
- the oracle checked persisted node states, typed artifact checksums/content,
  child review acceptance, fake SCM PR count and unchanged control checkout.

The fake SCM proves the fixture boundary only. Provider-independent remote PR
receipt/reconciliation is not claimed.

Full release verification PASS after the acceptance changes:

- `gofmt -w cmd internal sdk reference tests`;
- `go test ./... -count=1`;
- `go test -race ./... -count=1`;
- `go vet ./...`;
- `make check`;
- `./scripts/verify.sh` with TypeScript compiler smoke `PASS`.

The MCP detached-plan cleanup test uses a 15-second condition budget because
the previous 5-second budget was crossed only by full package-level contention;
the focused test remains fast and deterministic.

## Go production-shaped benchmark — 2026-08-10

Live-срез использовал пять изолированных Go-задач и внешний validator `gofmt + go test + race + vet`. Это production-shaped corpus, а не обезличенные production-данные. Workspaces создавались вне Git-репозитория; метрики времени не интерпретируются из-за неравномерной загрузки provider.

| Host / run | Strategy | success@1 | Final success | Attempts to valid | Input / output tokens |
|---|---|---:|---:|---:|---:|
| Pi 0.83.0, repeat=3 | direct | 14/15 (0.9333) | 14/15 (0.9333) | 1.0 | 458,463 / 19,285 |
| Pi 0.83.0, repeat=3 | feedback repair | 14/15 (0.9333) | 15/15 (1.0) | 1.0667 | 508,946 / 20,627 |
| OpenCode 1.18.14, direct Coder-Next repeat=1 | direct | 5/5 (1.0) | 5/5 (1.0) | 1.0 | 322,333 / 4,663 |
| OpenCode 1.18.14, direct Coder-Next repeat=1 | feedback repair | 5/5 (1.0) | 5/5 (1.0) | 1.0 | 280,694 / 3,844 |
| OpenCode 1.18.14, direct Coder-Next repeat=3 | direct | 15/15 (1.0) | 15/15 (1.0) | 1.0 | 901,816 / 13,800 |
| OpenCode 1.18.14, direct Coder-Next repeat=3 | feedback repair | 13/15 (0.8667) | 15/15 (1.0) | 1.1333 | 1,016,817 / 14,197 |
| OpenCode 1.18.14, direct Qwen 3.6, isolated repeat=1 | direct | 0/5 | 0/5 | — | 46,533 / 592 |
| OpenCode 1.18.14, direct Qwen 3.6, isolated repeat=1 | feedback repair | 0/5 | 5/5 (1.0) | 3.0 | 582,522 / 7,412 |
| OpenCode 1.18.14, direct Qwen 3.6, repeat=3 | direct | 0/15 | 0/15 | — | 139,554 / 1,091 |
| OpenCode 1.18.14, direct Qwen 3.6, repeat=3 | feedback repair | 0/15 | 0/15 | — | 433,960 / 1,856 |
| OpenCode 1.18.14, `aihub-proxy`, repeat=3 before SSE fix | direct | 0/15 | 0/15 | — | 130,380 / 1,216 |
| OpenCode 1.18.14, `aihub-proxy`, repeat=3 before SSE fix | feedback repair | 0/15 | 6/15 (0.4) | 3.0 | 694,805 / 7,720 |
| OpenCode 1.18.14, `aihub-proxy`, post-SSE-fix repeat=1 | direct | 0/5 | 0/5 | — | 46,568 / 717 |
| OpenCode 1.18.14, `aihub-proxy`, post-SSE-fix repeat=1 | feedback repair | 0/5 | 0/5 | — | 145,067 / 1,411 |

- Pi использовал `aihub/Qwen/Qwen3.6-27B`. Единственный `GOFMT_FAILED` в `01-cli-separator`, repeat 3 был восстановлен одним exact resume; repair получил пять stable-valid cases.
- Текущий OpenCode benchmark использует прямой `aihub-sbt/Qwen/Qwen3-Coder-Next`. Smoke дал `5/5 → 5/5`; единственный полный `repeat=3` — `15/15 → 15/15`, пять stable-valid cases, `0` failed executions. В repair два `GOFMT_FAILED` (`01-cli-separator`, repeats 2 и 3) восстановлены exact resume; остальные 13 outcomes были valid с первой попытки.
- Прямой OpenCode-контур использовал `aihub-sbt/Qwen/Qwen3.6-27B`; его isolated smoke и полный прогон имеют одинаковые matrix, strategy и benchmark fingerprints. В smoke третьи попытки дали настоящие `tool_use` и исправили 5/5 cases, а в полном `repeat=3` все 15 repair outcomes остались невалидными.
- Экспериментальный OpenCode/Qwen 3.6 proxy-контур использовал `aihub-proxy/Qwen/Qwen3.6-27B`. В полном proxy `repeat=3` direct дал `0/15`, repair — `6/15`; все шесть успехов появились на третьей попытке, `04-terminal-precedence` был valid `3/3`, а `03-exact-resume` и `05-persistence-error` — invalid `3/3`.
- Все 43 tool calls полного proxy-прогона были native `chatcmpl-tool-*`; schema-validated compact rewrite не сработал ни разу. Неуспехи без tool calls содержали stop/text/HTML, но не полный безопасно восстанавливаемый вызов.
- Proxy `repeat=3` обнаружил три transport failure вида `JSON chunk + [DONE]` без SSE-разделителя и один внешний `Unauthorized`. После узкого исправления SSE отдельный repeat=1 выполнил 20 OpenCode executions без parse/adapter/provider failure; все `0/5 → 0/5` остались честными `SCOPE_INVALID/attempts_exhausted` без tool calls.
- Полный repeat=3 после transport fix не перезапускался ради улучшения quality result; post-fix repeat=1 является диагностикой исправленного transport, а не заменой matrix evidence. Устойчивое преимущество repair для OpenCode не заявляется.
- Все сохранённые matrix reports завершились `passed: true`, fingerprints стратегий внутри matrix совпали по benchmark identity, исходный template остался неизменным. Adapter сообщил cost `0`; экономический вывод не делается.

## Live host conformance — 2026-08-10

Обе live-проверки использовали Qwen 3.6 27B; credentials, provider configuration и Session ID не сохранялись.

| Host | Version | Adapter fresh | Adapter resume | Extension load | Command | Input | Tool | Recovery | Completion |
|---|---|---|---|---|---|---|---|---|---|
| Pi | 0.83.0 | PASS | PASS | PASS | PASS | NOT VERIFIED | NOT VERIFIED | NOT VERIFIED | NOT VERIFIED |
| OpenCode | 1.18.14 | PASS | PASS | PASS | PASS | PASS | NOT VERIFIED | PASS | NOT VERIFIED |

- Pi adapter: `NODE_OPTIONS=--use-system-ca`, provider `aihub`, model `Qwen/Qwen3.6-27B`; fresh attempt returned version/Session ID, resume preserved the exact Session ID, and the real extension displayed the `/takt` preview/confirmation before any main-model response.
- OpenCode adapter: provider `aihub-sbt`, model `Qwen/Qwen3.6-27B`, agent `build`; fresh attempt returned version/Session ID and resume preserved the exact Session ID.
- OpenCode `1.18.14` loads plugins through `Plugin(input) -> Promise<Hooks>`. The entrypoint is compiler-checked against that contract; live `chat.message` interception created a durable `preview/guarded` session, blocked subsequent input, remained fail-closed while the daemon was unavailable and recovered through durable `host find` after restart.
- OpenCode headless mode keeps the intentional abort as generic `UnknownError` in NDJSON, while the plugin now writes the exact Takt preview/block reason to stderr before aborting. The live stdin smoke confirmed the diagnostic and absence of main-model text. Initial positional `opencode run <message>` is not a supported interception path because the host may submit it before external plugins settle; adapter and headless contract prompts use stdin.
- The first live routers exposed that `output_format` was validated but not sent to bundled assistants. Runtime now appends the exact contract to the prompt; both OpenCode and Pi returned a valid `TaskRoute` on attempt 1 without `router_fallback`.
- Reproduced implementation defects were fixed: obsolete OpenCode registrar API, policy deny misclassified as transport outage, common CLI flags appended after `--`, hidden headless diagnostics, and a stale `check-docs.sh` source assertion. TypeScript runtime/assignability contracts and Go regressions cover them.
- Bundled integrations remain `guarded`: no live evidence was obtained for OpenCode tool/completion blocking or for Pi input/tool/recovery/completion blocking.

## Post-audit repair — 2026-08-10

PASS на ветке `fix/release-gate-validation`:

- `go mod tidy` восстановил обязательные indirect module/checksum records, после чего `go vet ./...` завершился успешно;
- новые `yamlcodec` regressions сначала воспроизвели принятие второго JSON/YAML document и invalid trailing JSON, затем прошли после single-document fix;
- новые `whenexpr` regressions сначала воспроизвели принятие unterminated/mismatched quoted literals, затем прошли после delimiter validation;
- `go test ./... -count=1` прошёл для всех пакетов и полного `tests/e2e`;
- `go test -race ./... -count=1` прошёл непрерывным aggregate запуском;
- required TypeScript smoke прошёл с изолированно установленным `typescript@5.7.2`; CI устанавливает ту же pinned версию и выставляет `TAKT_REQUIRE_TYPESCRIPT=1`;
- `make check` прошёл с обязательным TypeScript smoke и documentation gate;
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
- documentation gate PASS;
- no `/mnt/data` sandbox path or local dependency `replace` in the release tree;
- `go vet ./...` and `go build ./...` PASS under the sandbox-only external modfile described above;
- assistant registry, `whenexpr`, workflow validation, appapi, MCP and architecture contracts PASS;
- the complete `internal/runtime` package PASS with the test binary isolated from sandbox `GOFLAGS`;
- complete `tests/e2e` PASS;
- TypeScript host integration smoke PASS.

The first combined clean-archive test command was stopped by the outer command channel after runtime had already printed PASS; E2E and the TypeScript smoke were then run separately and passed.
