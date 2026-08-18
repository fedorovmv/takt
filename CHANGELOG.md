# Changelog

## Unreleased

- Evaluation SCM fixtures now support stateful `gh pr list --head/--state`
  checks, and the feature-flow PR receipt gate accepts at least one successful
  `gh pr create` instead of rejecting duplicate receipts from one assistant
  execution. Product version is `0.1.61-alpha`; the bundled `code` profile is
  `0.19.2`.
- Test gates are tiered: `make check` compiles all Go packages, runs focused
  architecture/profile/parser contracts, build, and TypeScript smoke; the
  ordinary E2E and full ordinary/race package suite remain in
  `make check-full`/`scripts/verify.sh`.
- Feature-flow artifact and verdict checks now use shared profile tools. Their
  complete missing/empty/directory and parser-failure matrices run as cheap Go
  profile contracts; full-flow E2E keeps representative branch/failure cases.
- Independent E2E contracts now run in parallel, and the route benchmark keeps
  one representative case/repeat while unit contracts retain full matrix
  aggregation and gate-failure coverage. `make e2e` uses 16 test workers by
  default.

## v0.1.60-alpha

- `progress.json` and `eval-status` now expose accumulated evaluation phase
  timings plus observed assistant wait/stream/total and tool-call durations;
  old progress snapshots remain readable with unavailable timing data.
- Cumulative Pi message timing diagnostics are now counted once per model call,
  so `LLM total` and `LLM stream` no longer double-count repeated completions.

## v0.1.59-alpha

- `code:feature-development` is now outcome-gated: a strict validation verdict
  (`PASS`, `REPAIR`, or `BLOCKED`) controls delivery, with at most one repair,
  independent revalidation, durable review evidence, fail-closed PR/summary
  artifacts, and exactly one successful PR receipt in evaluation fixtures.
- Mini-du validator 3 adds `hardlink_multiple` and `double_dash_default`.
  Validator-3 evaluations are generation v2 and must not be compared with
  registry-old generation v0 or validator-2 generation v1. Hook retry session
  authoring now accepts only `fresh`/`resume` with `action: retry`.
- Product version is `0.1.59-alpha`; the bundled `code` profile is `0.19.0`.

## v0.1.58-alpha

- `code:feature-development` now fails its implementation hook when the
  required `implementation.md` artifact is not a non-empty regular file, so the same assistant
  session is retried before review/PR work begins. Mini-du validation separates
  `missing_artifact` from product failures and checks failure diagnostics by
  exit/stdout/stderr contract instead of host-specific wording. Invalid flow
  results no longer expose a misleading per-run `time_to_valid_ms`.

- Версия продукта обновлена до `0.1.58-alpha`, профиль `code` — до `0.18.0`,
  authoring skill — до `0.40.0`. Корневая и runtime-версия теперь защищены
  contract test, а release-процесс зафиксирован в `AGENTS.md`.

- Pi-backed flow evaluation and advisory analysis now disable hidden Pi/SDK
  retries in their isolated workspaces, leaving Takt as the single durable
  provider-retry owner. Existing Pi RPC turn, first-stream, completion and
  auto-retry lifecycle observations are shown in live status/trace and retained
  as redacted `activity.json` evidence with client wait/stream/total timing.
  Pi assistants can supply a native `settings` object for isolated evaluation;
  mini-du pins `httpIdleTimeoutMs` to five minutes while eval-owned retry keys
  remain authoritative.

- Clarified the mini-du corpus contract that default numeric output and `-k`
  both match `du -k`. Validator mismatch diagnostics now retain bounded
  normalized candidate/oracle exits and outputs, while production validation
  and advisory analysis prompts require exact reference parity and a causally
  sufficient explanation of the observed delta.

- `takt eval inspect` now reports a live case from `progress.json` before the
  first `report.json` checkpoint. `takt eval analyze` fails clearly while the
  evaluation is still running, does not start an analyzer, and no longer prints
  a typed `<nil>` result before the error.

- Evaluation worktree inputs now remap control-local absolute paths to the
  corresponding execution worktree before assistant nodes run. Inspection no
  longer reports writes to the durable run-artifact directory as control
  workspace mutations. Production implementation/validation/PR prompts keep
  repository operations inside the execution workspace and do not search
  sibling workspaces or previous runs when optional artifacts are absent.

- Added read-only `takt eval analyze` over saved flow evaluations. The dedicated
  `takt_analyze` alias produces timestamped redacted manifests and structured
  advisory diagnoses while preserving deterministic outcomes and the original
  `report.json` byte-for-byte. Adding the alias intentionally changes future
  Config fingerprints; existing reports remain valid.

- Hardened evaluation analysis evidence: rendered prompts and fingerprints are
  persisted after redaction, source evidence is copied into bounded analysis
  workspaces, deterministic inspection context and validator diagnostics are
  included, citations are resolved against the manifest, and analyzer session
  evidence/trace are retained before cleanup.

- Evaluation analysis now accepts checked citations to the generated
  `evidence-manifest.json`, resolves relative analyzer session paths inside the
  execution workspace with symlink/escape rejection, and preserves bounded
  redacted raw model output for protocol failures. Citations may also repeat
  the manifest evidence-root prefix when the normalized suffix is listed, and
  common pointer/line citation variants are normalized before validation.
  Completed advisory results now require an evidence-backed causal mechanism,
  bounded failure point and concrete prevention instead of restating the
  deterministic validator verdict.

- `takt eval analyze` now accepts `--language en|ru` (default `en`) and stores
  the selected language in its report and manifest; JSON keys and enum values
  remain stable. `failure_mode` is now an untranslated lowercase snake_case
  machine code, keeping localized analysis reports comparable.

- `code:feature-development` now places an eval-only `pr-effect-gate` between
  `create-pr` and `summary`. When the SCM fixture is present, a missing
  recorded `gh pr create` fails the Run instead of producing a false accept;
  ordinary production SCM runs are unchanged.

- The evaluation fake `gh` now derives fixture and state paths from
  runtime-provided `TAKT_WORKSPACE`, with its committed `.takt/eval/bin/gh`
  location as fallback, so assistant `FAKE_GH_*` overrides cannot redirect the
  recorded SCM side effect even when a control-workspace copy is invoked.

- Flow evaluation now waits for the first durable Run state after detached
  start acceptance instead of failing when worktree preparation takes longer
  than the start response window.

- `eval-status` now shows fixed elapsed duration, case/node completion
  percentages, input/output/total durable tokens, current measured model
  context, and the quality valid rate for completed validation results.

- Исправлена классификация Pi `Connection error`: provider outage теперь
  попадает в `provider_unavailable` и отдельный provider retry scope, не
  загрязняя workflow retry budget и quality denominators.

- Pi `stopReason=length` now fails as an execution `exit` instead of accepting
  an empty `agent_settled` result. `code:feature-development` retries that exit
  up to three times with the same Session ID and explicit feedback; Pi model
  `maxTokens` remains separate from Takt's RPC byte limit.

- Added deterministic `takt eval inspect <output-dir>` and `make eval-inspect`
  failure investigation, per-case causes in `eval stats`, and redacted filtered
  `activity.json` tool-start evidence. A persisted-evidence `CAUSAL CHAIN`
  connects assistant output limits/tool activity to empty results, failed
  validation and skipped nodes. Inspection is read-only, never contacts a
  model and never changes the validator verdict. A progress publication failure
  now cancels or joins the detached case Run before returning its error.

- Reworked human `eval compare` into an A/B scorecard with explicit overall,
  correctness/reliability/efficiency and per-row assessments, humanized resource
  deltas, presets/models and case transitions. Validity has priority over
  efficiency, and missing values are no longer printed as meaningless `null%`.
  JSON compare reports now retain both evaluation output directories.

- Added atomic production-flow `progress.json`, `takt eval status <output-dir>`
  and `make eval-status RUN=...` for an external live view of suite phase,
  durable Run/node progress and measured persisted usage. Status inspection
  never starts workflows or models; final snapshots remain beside reports and
  expose stale interrupted runs through `updated_at`.

- Flow evaluation now preserves the redacted final product source tree and a
  baseline-to-final diff before cleanup, plus a secret-checked full-history
  `repository.bundle`. The mini-du oracle ignores `.git/` and `.takt/`,
  preventing bundled profile tools from being misclassified as candidate
  delegation; oracle invocations are allowed only in `_test.go` files.

- Live flow trace now uses `SCOPE | EVENT | DETAILS`, puts short Run/node context
  first, announces full Run/Session IDs once, removes repeated model/session
  fields from tool traffic, and distinguishes validation/cleanup checkpoints
  from the finalized report write. Active heartbeats show the last measured Pi
  model-request input tokens as context, or explicit `context=unknown`; final
  cumulative attempt usage is not mislabeled as context size.

- mini-du feature evaluation now has an explicit per-case CLI contract for
  `-s`, `-k`, `-H`, `-h`/`--help`, `--`, combined `-sk`/`-ks`/`-sH`, and invalid
  options. `eval-feature-smoke` runs one case; `eval-feature` runs the full
  feature corpus. `-H` uses binary humanized units (`0B`, one decimal below 10,
  rounded integers from 10).

- Added `takt eval stats <output-dir>` and `make eval-stats RUN=...` for compact
  saved-run statistics. Flow comparison now includes input/output/total tokens,
  attempts and duration; `make eval-compare A=... B=...` compares existing runs
  only, with deltas defined as `B-A`. Stats now separates node attempts from
  assistant executions and reports per-assistant-step wall time captured from
  durable node events; old reports remain readable with unavailable timing.
  Full per-execution Session IDs are shown separately with workflow/provider
  attempt and fresh/resume mode for assistant-native inspection.

- Added durable `provider_unavailable` recovery: up to three same-session Takt
  adapter calls per workflow attempt (`2s`/`4s`, capped `Retry-After`), separate
  provider/workflow attempt evidence and retry lifecycle events. Flow reports
  retain outage diagnostics/usage as `infrastructure_error` outside quality
  denominators; domain side effects are excluded. Provider backoff/resume retain
  one absolute node deadline and terminal execution diagnostics.

- Serialized Run Store state/event/index reads and writes with portable native
  file locks on Unix, AIX/Solaris and Windows; stale `Save` snapshots now fail
  closed instead of overwriting a newer event revision.

- Added shared Config `model_preset`/`model_presets` with arbitrary aliases,
  durable selection for run/command/eval, effective-model fingerprints, and
  generic `MODEL_<ALIAS>` overrides for comparisons without editing workflows.

- Added `make eval-smoke`, `eval-feature`, `eval-review`, and `eval-architect`
  as short entrypoints for the bundled live Pi evaluation corpus.

- Flow trace now identifies suite/workflow/config/model/output, emits bounded
  live Pi tool/message progress for root and child Runs, reports the measured
  outcome/validator diagnostic, and reduces unchanged node status to 30-second
  `node.active` lines. `eval flow` also supplies a configurable 5-minute idle
  fallback, so a provider that stops after a tool result fails as `timed_out`
  instead of keeping the evaluation alive indefinitely.
- Pi streaming/tool updates now reset assistant inactivity without entering
  durable output. Flow heartbeats show root/child Run ID, idle duration, limit,
  last activity and whether Takt is waiting for provider streaming or response.

- Pi RPC больше не сохраняет кумулятивные `message_update`, transient tool
  updates и fire-and-forget extension UI в durable stdout; одиночная oversized
  запись всё ещё fail-closed. `takt eval flow --trace` показывает suite stages,
  durable root events, terminal child statuses и текущий running node в stderr
  без загрязнения JSON.

- Исправлены контракты production-flow evaluation: fingerprint повторов больше не
  зависит от абсолютного пути remote, `require_push` проверяет текущую ветку,
  baseline mutation имеет приоритет над timeout/cancel, default output получает
  UTC timestamp, JSON input передаётся через профильный файл, а отрицательный
  `unstable_cases.max` отклоняется. Detached start сохраняет ранний ответ для
  non-waiting durable state и не гоняет immediate approval answer с persistence.

- Added `takt eval flow` isolated production-flow suites, durable repeat evidence,
  and `takt eval flow init` validator-free scaffolding.

- Уточнены shell-контексты для single-quoted `$(` и `case` pattern без
  рассинхронизации quote-state; `fresh_context` теперь действует только на
  первую попытку итерации `N > 1`, а retry продолжает управляться
  `attempts.retry_session`.

- Закрыты дополнительные REQUEST CHANGES: shell lexer учитывает комментарии,
  heredoc и вложенные command substitutions; `cancel` синхронизирован с
  loop-child schema; запрещены root `fresh_context`/`until_bash`, а
  `fresh_context` имеет приоритет над shared session. Исправлены canonical paths
  expanded loop children и static `foreach` examples (`$INPUTS.<as>`).

- Закрыты review-контракты Archon A1: redaction durable loop evidence и
  cancel metadata, quote-state shell rendering с fail-closed `BASE_BRANCH`,
  raw inline scripts через argv/env boundary, predicate snapshots/safe-stop,
  nearest shared-session selection, foreign-signal ambiguity и schema placement
  loop fields. README workflow examples переведены на target dialect.

- Реализован Archon-first A0/A1 Workflow contract: target root/provider/model,
  единый `$...` reference lexer/rendering и fail-closed rejection legacy
  `${...}`, `assistant` frontmatter и `apiVersion/kind/metadata/defaults`;
  `output_format` сохранён без изменения semantics.
- Добавлены bounded repair loops: scalar/structured `until`, signal
  `matched_signal`/`signal_diagnostic`, `until.requires`, `until_bash`, durable
  loop history, `fresh_context`, `context: shared`, exact Session ID resume,
  approval continuation и cancel/retry evidence. Hard budgets, `run inspect` и
  mutating merge fan-out остаются deferred.
- `code:plan-to-pr` получил обязательный user-owned `allowed_paths`, native Git
  scope checks до draft PR и после review, а также fail-closed PR/review/summary
  gates. Go E2E покрывает один `safe_success` и семь `safe_stop` outcomes.
- MCP detached-plan cleanup теперь выдерживает aggregate package contention;
  production lifecycle semantics не менялись.
- Удалены `MANIFEST.sha256` и отдельный `verify-manifest.sh` gate; `make check`
  и `scripts/verify.sh` больше не поддерживают дублирующий checksum-манифест.
- Удалён `scripts/check-docs.sh`: hardcoded hashes и проверки наличия отдельных
  строк не являются проверкой корректности документации; продуктовые контракты
  остаются в Go-тестах.

## v0.1.57-alpha

- Добавлен пятизадачный production-shaped Go benchmark с внешним `gofmt/test/race/vet` validator и direct/feedback-repair стратегиями. Default output вынесен из Git-root в `${TMPDIR:-/tmp}/takt-go-benchmark/evals` и закреплён поведенческим Go-тестом.
- Pi/Qwen 3.6 `repeat=3` дал `14/15 → 15/15` с одним exact resume. OpenCode/Qwen3-Coder-Next дал direct `15/15` и repair `13/15 → 15/15` с двумя exact resume после `GOFMT_FAILED`. Сохранённый OpenCode/Qwen 3.6 evidence хуже: direct `0/15 → 0/15`, proxy `0/15 → 6/15` до исправления SSE transport defect и post-fix smoke `0/5 → 0/5` без tool calls; влияние compact rewrite не заявляется.
- `attempts_exhausted` больше не затирает output, Session ID и `resumed` последней фактической execution; regression покрывает exhausted hook retry.
- Live smoke на Qwen 3.6 27B подтвердил fresh/exact resume Pi `0.83.0` и OpenCode `1.18.14`; Pi command interception и OpenCode command/input/recovery проверены через реальные host entrypoints.
- OpenCode host plugin приведён к API `Plugin(input) -> Promise<Hooks>` версии `1.18.14` и защищён TypeScript assignability/runtime contract; policy deny больше не считается transport outage, exact block diagnostic выводится в stderr, а common CLI flags у Pi/OpenCode ставятся до `--` и не попадают в goal.
- `output_format` теперь добавляется к assistant prompt как точный JSON contract. После исправления Pi/OpenCode live routers на Qwen вернули валидный `TaskRoute` с первой попытки вместо protocol failure/fallback. Bundled host status остаётся `guarded`; `strict` и непроверенные tool/completion boundaries не заявляются.
- Documentation gate больше не проверяет устаревшие TypeScript symbols через `grep`; host behavior проверяется Go E2E и обязательным TypeScript smoke.
- Post-audit repair restored the clean-checkout Go module graph, single-document YAML/JSON authoring, and malformed quoted `when` rejection.
- GitHub Actions now installs pinned TypeScript 5.7.2 and requires host-integration compilation; local Go-only checks may still skip it when the compiler is absent.
- Feature freeze сохранён: новых Workflow/Run/CLI/MCP возможностей не добавлено; релиз фиксирует три architecture contracts после сравнения с идеологическим родителем Archon.
- Принята конституция языка workflow: **YAML координирует. Код вычисляет. Агент принимает решения.** `when` централизован в `internal/whenexpr`, валидируется до Run и остаётся намеренно ограничен `==`, `!=`, `&&`, `||` без incremental expression creep.
- Bundled assistant integrations объявляют `ProviderRegistration`; immutable provider registry собирается единственным production composition root в `internal/bootstrap`, без global mutable registry или `init()` registration.
- `internal/appapi.OperationDescriptor` стал schema-first источником canonical operation ID/stage/MCP metadata/InputSchema/annotations/docs; вход валидируется той же schema до generic typed decode.
- MCP canonical tools проецируются из appapi descriptors. `docs/71-canonical-operation-contracts.generated.md` генерируется из них же и защищён drift-test.
- Architecture gate запрещает второй `when` parser, production provider-registry assembly вне bootstrap и расхождение canonical MCP/appapi boundary.
- Добавлены ADR-090 и `docs/72-architecture-contracts-v0.1.57.md`; основной `docs/04-architecture.md` обновлён нормативными правилами. Takt skill обновлён до 0.39.0.

## v0.1.56-alpha

- Feature freeze завершён codebase-hygiene проходом: функции не удалялись и публичные Workflow/Run/SDK operation names не расширялись.
- `takt-schema-subset/v1` сохраняет Takt-specific subset policy, а instance validation теперь делегирован `github.com/santhosh-tekuri/jsonschema/v6` Draft 2020-12; собственный production/test JSON Schema runtime удалён.
- Fake assistant/code/domain/Pi/OpenCode binaries перенесены из product `cmd/` в `internal/testsupport/cmd`; E2E по-прежнему собирает их под историческими именами.
- Pi/OpenCode implementations перенесены из stable `internal/assistant` в `internal/extensions/assistants`; provider-neutral factory получает их через bootstrap injection.
- Capability preflight использует injected assistant resolver, а logical `coding-agent` configuration validation больше не зависит от provider implementation.
- Декомпозированы стабильные hotspots process v1alpha2, script execution и governed child Run без изменения durable/wire semantics.
- External worker/tool lifecycle вынесен из `internal/application` в `internal/externalworker`; общие durable lock/redaction/reload helpers централизованы в `internal/runcontrol`, application уменьшен примерно до 2,2 тыс. production-строк.
- CLI help разделяет stable/extensions/experimental/tooling; MCP Dynamic Flow и Host Control явно маркируются Experimental, compatibility matrix отражает тот же статус.
- Architecture gate закрепляет отсутствие fake product commands, provider-specific assistant code в core и собственного JSON Schema engine.
- Добавлены ADR-089 и `docs/70-codebase-hygiene-stabilization-v0.1.56.md`; Takt skill обновлён до 0.38.0.

## v0.1.55-alpha

- Feature freeze продолжен: существующий функционал физически разделён на stable core, `internal/extensions`, `internal/experimental` и `internal/tooling` без удаления CLI/MCP возможностей.
- Dynamic Flow (Router/Dynamic Plan/replan/evidence), Host Control и Learning Loop переведены в experimental boundary; stable application/runtime/workflow/config/profile/store не импортируют experimental/tooling/extensions.
- Package Distribution, Block Catalog и Notifications оформлены как extensions; evaluation/compatibility вынесены в tooling. Profile resolution больше не зависит от package manager, installed packages подключает extension-aware catalog loader.
- `internal/application` уменьшен примерно с 6,8 тыс. до 3,5 тыс. production-строк за счёт вынесения самостоятельных модулей, а не удаления функций.
- Самописный `internal/yamlmini` удалён. YAML syntax делегирован `go.yaml.in/yaml/v3 v3.0.4`; Takt сохраняет небольшой `yamlcodec` для strict JSON-tag contract diagnostics.
- Добавлен `make journeys`: black-box gate `init/validate/run/status/events/artifacts`, approval/answer, failure/retry и reusable subworkflow через настоящий CLI.
- CLI usage группирует команды как stable/extensions/experimental/tooling.
- Architecture gate закрепляет односторонние module dependencies и запрет возврата самописного YAML parser.
- Добавлены ADR-088 и `docs/69-core-stabilization-modularization-v0.1.55.md`; Takt skill обновлён до 0.37.0.

## v0.1.54-alpha

- Feature freeze продолжен: продуктовые/public contracts не расширялись; выполнен повторный architecture hardening после независимого аудита `v0.1.53`.
- Удалён shared `application.Context`; все application services имеют private dependencies, concrete stores/backends/factories собираются в `internal/bootstrap`.
- Разорван цикл `RunService ↔ PlanService`: cross-lineage fork вынесен в `ForkService`; architecture gate теперь строит service dependency graph и запрещает циклы.
- Production runtime больше не имеет default/hidden constructor; child workflows наследуют injected dependencies, evaluation получает execution factory через bootstrap.
- Signal-aware root context проходит `cmd/takt → CLI → application → runtime`; durable detached operations используют явный `detachedContext`, а direct `context.Background()` в application ограничен документированным recovery helper.
- Удалены process-global `dynamicMu/hostMu`; Dynamic Plan и Host Control координируются durable store locks.
- Разложены Task response, Dynamic Plan advance и governed child fan-out state machines без нового plugin/DI framework.
- Canonical app operation identity централизована в `internal/appapi`; MCP использует тот же mapping вместо отдельной таблицы.
- Оставшиеся package/reference/host/deep-workflow shell contracts перенесены в bounded Go E2E; `scripts/test-*.sh` содержит только TypeScript compiler smoke.
- Добавлены ADR-087 и `docs/68-architecture-hardening-v0.1.54.md`; Takt skill обновлён до 0.36.0.

## v0.1.53-alpha

- Feature freeze продолжен: продуктовые возможности не добавлялись; тестовый контур переработан после application-boundary refactor.
- Основной источник product correctness теперь стандартный `go test ./...`; black-box CLI/daemon/MCP/evaluation проверки перенесены в `tests/e2e` и запускают реальные Takt binaries через общий Go harness.
- 38 исторических `scripts/test-*.sh` сокращены до пяти bounded smoke-сценариев только для внешних process/language/package boundaries; architecture test закрепляет этот allowlist и запрещает рост второго shell test framework.
- Schema registry/offline contract перенесён из Python/shell в `internal/schemacontract`; Draft 2020-12, локальные `$ref` и регистрация схем проверяются Go-тестом.
- Удалены test-only `evalassert`/`routee2eassert`; JSON/JSON-RPC, fixtures, temp projects и process assertions переиспользуют `tests/e2e` harness вместо встроенных Python/grep helpers.
- Исторические Make targets сохранены как совместимые входы, но маршрутизированы в Go packages/tests; `make e2e` и `make smoke` явно разделяют black-box product tests и внешний smoke layer.
- Монолитный deep-code shell regression сокращён до representative Git/fake-gh/process-agent smoke; валидность полного каталога 19 code workflows закреплена Go E2E и runtime/application suites.
- Добавлены ADR-086 и `docs/67-go-native-test-architecture-v0.1.53.md`; Takt skill обновлён до 0.35.0.

## v0.1.52-alpha

- Feature freeze release: продуктовые возможности не добавлялись; проведён application/transport/runtime refactor по SOLID/DRY/KISS/YAGNI перед дальнейшим развитием.
- `cmd/takt` превращён в тонкий launcher; CLI перенесён в `internal/cli` и разделён на небольшие command adapters по областям ответственности.
- Монолитный `internal/control.Service` удалён. Use cases разделены на Run/Plan/Task/External/Host/Catalog/Authoring/Worktree/Command/Notification/Maintenance/Evaluation/Learning/Compatibility/Adapter/Package services в `internal/application`.
- Добавлен единственный production composition root `internal/bootstrap`; application persistence инвертирован через consumer-owned `RunStore`, production implementation остаётся `store.FS`.
- Daemon переведён на общий `internal/appapi` operation registry; MCP использует тот же registry для общих операций и получает узкие `API/Plan/External/Maintenance` dependencies.
- Background lifecycle daemon/MCP унифицирован через `MaintenanceService.Tick`; transport-specific business loops удалены.
- Runtime получил явные `Definition + Dependencies` и `NewWithDependencies`; dependency fields `Runner` стали private, attempt lifecycle и node action implementations вынесены из scheduler orchestration без введения plugin framework.
- CLI evaluation/learning/package/compatibility/adapter operations переведены за application boundary; evaluation task matrix использует injected application callback вместо самостоятельной сборки control plane.
- Монолитные workflow/config/learning validators разложены на тематические проверки с сохранением error semantics.
- Добавлен архитектурный regression gate `scripts/test-architecture.sh`, ADR-085 и `docs/66-application-boundary-architecture-refactor-v0.1.52.md`; Takt skill обновлён до 0.34.0.

## v0.1.51-alpha

- Открыт P3: добавлен human-reviewed Skill/Block Learning Loop `takt learn scan|propose|list|get|review|evaluate|stage` поверх durable Run history без нового runtime/workflow node.
- Learning proposal `takt-learning/v1alpha1` хранит repeated fingerprint, supporting Run IDs, expected benefit, immutable candidate SHA-256, human rationale и matrix evaluation provenance; schema опубликована как `learning-proposal.schema.json`.
- `stage` требует human accept и passing regression gates, повторно проверяет candidate hash и пишет только `.takt/learning/ready`; trusted package/skill configuration автоматически не меняется.
- Исправлен fork structured Task Source provenance и crash-window после durable satisfied loop iteration; добавлены resume regression на satisfied boundary и legacy state без `loop_iterations`.
- MCP Domain Describe получил bounded default timeout и текущую client version; GitHub SCM discovery получил timeout, GitHub Task Source URL ограничен `github.com`, пустой process `event_types` закреплён как deny-all.
- Compatibility mini-validator стал fail-closed по schema keywords и использует JSON-number equality; добавлен meta-schema ↔ `schemasubset.Description()` contract test.
- Исправлен Task Source README, CLI JSON envelope закреплён schema/test, control-level Task Source flow покрыт unit-test, `verify-manifest.sh` включён в `make check`.
- Добавлены `scripts/test-learning-loop.sh`, `docs/65-human-reviewed-learning-loop-v0.1.51.md`; Takt skill обновлён до 0.33.0.

## v0.1.50-alpha

- Добавлен `takt-task-source/v1alpha1`, public `sdk/tasksource`, `task_sources` в config и `task start --source/--source-ref`; normalized Task с immutable source revision передаётся Router/Planner/Replanner.
- Добавлен reference GitHub Issue source (`reference/githubtask`, `takt-github-task-source`) без imports из `internal/`.
- Закрыт iteration-history crash/retry debt: resume продолжает после durable history, bounded history не превышает 64, добавлены foreach/governed-child/backward-compat regressions.
- Schema subset получил поведенческие input/output/unsupported-keyword tests, расширенное keyword coverage и numeric equality для `uniqueItems`; schema registry проверяется offline и без cross-file refs.
- Pi/OpenCode probes, Domain Describe и reference GitHub commands получили bounded timeouts; MCP domain env поддерживает `secret://`; Qwen budget timeout нормализован через `failure_kind`.
- Release manifest теперь проверяется `scripts/verify-manifest.sh`; временные `.tmp/.swp/.bak` файлы запрещены в поставке.
- Добавлены ADR-082/083 и `docs/64-structured-task-sources-v0.1.50.md`.

## v0.1.49-alpha

- Добавлен `cmd/qwen-takt-adapter` как первая public-SDK-only reference implementation `takt-assistant/v1alpha2`: Qwen Code headless `stream-json`, model selection, fresh/exact resume, timeout, session/message/usage/terminal normalization и fail-closed unsupported Takt policy.
- Process `v1alpha2` больше не выводит `tool_control` и остальные event/control guarantees из версии протокола; configured capabilities должны подтверждаться фактической stream declaration, undeclared event/tool request отклоняются.
- Добавлен `cmd/takt-github-scm-adapter` как reference implementation neutral SCM contract через authenticated `gh`: repository/change/check reads, change create/comment/review и reconcile.
- Public `sdk/domainadapter.InvokeRequest`/`ReconcileRequest` получили execution `workspace` и request validators; process и MCP transports применяют один validation/cwd contract, а multi-repo publication передаёт `repository_workspace` candidate worktree.
- GitHub mutating operations используют SHA-256-derived idempotency marker и reconcile после ambiguous transport failure без повторной mutation.
- Добавлены `reference/qwencode`, `reference/githubscm`, `examples/reference-adapters`, `scripts/test-reference-adapters.sh`, ADR-080/081 и `docs/63-reference-external-adapters-v0.1.49.md`.
- P2 backlog/roadmap/status обновлены: external wrapper и production-like SCM reference закрыты; остаются live host conformance и structured task source adapter.
- Takt skill обновлён до 0.31.0; профиль `code` остаётся 0.16.0.

## v0.1.48-alpha

- Зафиксирован общий structured JSON contract `takt-schema-subset/v1` для `input.schema` и `output_format`; полный JSON Schema не заявляется, а unsupported keywords fail-closed проверяются общим validator.
- Добавлены `takt compatibility matrix|fields|schema|check`: session adapters, host integrations и domain adapters имеют раздельные support/verification статусы; `--live` выполняет доступные probes, `--strict` даёт CI/preflight gate.
- Добавлен машиночитаемый field-by-field audit будущей границы `v1beta1`; contract-test ломается при незадокументированном изменении публичных JSON-полей.
- `takt-assistant/v1alpha1` формально помечен deprecated для новых process wrappers; целевой process protocol — `v1alpha2`, legacy чтение сохраняется в v0.2.
- Добавлены JSON Schema для schema subset, compatibility matrix/check и v1beta1 field matrix, а compatibility E2E включён в `make check` и `scripts/verify.sh`.
- Roadmap/backlog/status уточнены: финальная migration к `v1beta1` остаётся после production evidence, а live host conformance/reference adapters вынесены во внешние seams.

## v0.1.47-alpha

- Начат стабилизационный этап `v0.2`: опубликован contract audit `stable-candidate | supported-alpha | deprecated | internal` и draft migration policy `v1alpha1 → v1beta1`.
- `loop_group` сохраняет first-class durable `loop_iterations[]` со snapshot всех завершённых итераций; `loop_previous` остаётся совместимым представлением последней итерации.
- `loop_group.max_iterations` ограничен `1..64`; nested `loop_group` явно остаётся вне `v0.2`, остальные виды композиции внутри loop сохраняются.
- Исправлен остаточный redaction gap: control строит redactor из `RunState.config_path`, включая profile-resolved/per-run config override; runtime и control используют единый config-secret constructor.
- Evaluation approval path больше не коммитит live unredacted state; validation report `output_path` редактируется до записи; control operations используют общий redacted commit helper; `WorkflowPlan` persistence из control также редактируется по фактическому `record.config_path`.
- Task-level evaluation выровняла `min_plan_revisions` (`1` = initial plan, replan начинается с `2`), durable needs-input, final-success/error semantics, repeat>1 E2E и workspace fingerprint/copy boundary.
- Ужесточены task/evaluation matrix report schemas: основные nested summary/run/pairwise/compare records типизированы вместо свободных `object`.
- Добавлен `scripts/test-iteration-history.sh` и релизный gate на iteration history/limit.
- Takt skill обновлён до 0.29.0; профиль `code` остаётся 0.16.0.

## v0.1.46-alpha

- закрыт общий persistence redaction для control/external worker paths, approval, external tool I/O/results/artifacts и domain receipts;
- templated SecretRef регистрируется после render adapter env;
- external non-text artifact с known secret блокируется fail-closed;
- добавлен `takt eval task-benchmark` для полного `Task Router → Dynamic Plan → replan` пути;
- добавлены `TaskEvaluationMatrix`, `TaskCaseManifest`, task report, pairwise outcomes и task-level regression gates;
- immediate retry events несут diagnostic fingerprint;
- `repeat: 0` больше не нормализуется молча;
- расширено покрытие stability/time-to-valid/failed-execution-cost/gates;
- добавлены task evaluation schemas и усилены matrix/compare report schemas;
- macOS portability: adapter-platform python3 fallback и sandbox-exec regression coverage;
- backlog, implementation status, roadmap, README и evaluation plan пересобраны вокруг evidence → stabilization → external seams → learning loop.
- Takt skill обновлён до 0.28.0; профиль `code` остаётся 0.16.0.

## v0.1.45-alpha

- Evaluation runner получил `EvaluationMatrix` и `CaseManifest`: `takt eval benchmark` запускает несколько agent-neutral workflow-стратегий на одном corpus/repeat, а `takt eval compare` строит парные переходы baseline/candidate и category breakdown.
- Добавлены true `time_to_valid_ms` по durable quality-node event, retry/failed-execution metrics, diagnostic fingerprints и stable/unstable case aggregation.
- Добавлены regression gates для success@1/final success, cost/time regression и unstable cases; gate failure возвращает non-zero после сохранения полного `benchmark.json`.
- Route DSL corpus расширен до 25 cases: 10 regression + 15 production-shaped synthetic cases. Добавлены baseline-direct, feedback-repair и inspect-feedback стратегии; конкретный coding-agent выбирается конфигурацией, а не workflow.
- Добавлен воспроизводимый `scripts/test-route-dsl-benchmark.sh`, который доказывает baseline→candidate improvement на fake Route DSL model/validator и отрицательный gate path без Python dependency.
- Добавлены ADR-072/073, `docs/59-route-dsl-evaluation-strategy-benchmark-v0.1.45.md` и новые evaluation schemas. Takt skill обновлён до 0.27.0; профиль `code`/`code-core` не меняются.

## v0.1.44-alpha

- Добавлены durable retry/backoff (`attempts.backoff`) и machine-readable diagnostics с нормализованным SHA-256 fingerprint; retry decision хранит точный `not_before` и переживает restart/resume.
- Governed fan-out получил раннее завершение: `one_success` и `all_success` отменяют ненужные siblings с отдельной причиной `fanout_result_decided`; `all_done` сохраняет полное ожидание.
- Добавлены `secret://ENV_NAME`, централизованный redaction durable state/events/text artifacts, durable-only public foreground state и fail-closed запрет non-text artifact с известным secret. Explicit SecretRef защищается независимо от длины значения.
- `bash/script` получили локальный OS sandbox `enforcement: required|optional`: `bubblewrap` на Linux, `sandbox-exec` на macOS при наличии. Validation runtime и hooks используют тот же node-level enforcement; command/prompt остаются честным assistant-enforced contract.
- `NodeState.path` и `node_path` events добавляют канонический path namespace для вложенной композиции без смены совместимых node IDs.
- Исправлены замечания v0.1.43: macOS symlink auto-discovery, resolved child paths, python3 fallback, настоящий topological merge order, пустой Workspace fail-closed, Git allowlist boundary, adapter-doctor negative regression, multi-repo deny/replanner/fingerprint/brief/node-rule tests. Completed governed child теперь переиспользуется при retry родительского post-check.
- Добавлены ADR-070/071, `docs/58-runtime-reliability-local-security-v0.1.44.md` и `scripts/test-runtime-reliability-security.sh`. Takt skill обновлён до 0.26.0; профиль `code` и `code-core` не меняют содержимое и остаются 0.16.0 / 0.5.0.

## v0.1.43-alpha

- Добавлен bounded multi-repo workspace catalog (`.takt/workspace.yaml`) с Git repository IDs, dependency graph, automatic local discovery и fingerprint HEAD/path/dependencies.
- Dynamic WorkflowPlan получил `repository` и `publish_change`; Router/Planner/Replanner получают repository catalog и adapter preflight, а Go validation проверяет реальные repositories, dependency order и одного mutating owner на repo.
- Repository phases используют обычные governed child Runs с отдельными managed worktrees. Parent state сохраняет child workspace/branch/base commit; completed repository phases сохраняются при retry/replan.
- Добавлены per-repository candidate SHA/EvidenceManifest, aggregation actual Git diff как `repo/path`, общий multi-repo candidate fingerprint, trusted `repository-change` и `integration-verify` blocks.
- `publish_change` компилируется в provider-neutral `scm/change.create` с существующим idempotency/reconciliation contract; план хранит output каждого change и deterministic merge order.
- Закрыты замечания v0.1.42: security-negative package tests, path-boundary allowlist, scope-aware dependencies, npm-compatible caret для 0.x, tested lock rollback, executable portable-package example, non-zero adapter doctor, process stderr lifecycle и дополнительные conformance/package regressions.
- Добавлены ADR-068/069, `schemas/workspace.schema.json`, `docs/57-multi-repo-dynamic-workflows-v0.1.43.md` и `scripts/test-multi-repo.sh`. Профиль `code` обновлён до 0.16.0, `code-core` — до 0.5.0, Takt skill — до 0.25.0.

## v0.1.42-alpha

- Добавлена Portable Package Distribution поверх существующего `BlockPackage`: `takt package install|update|uninstall|list|sync|doctor|sign`, local/Git sources и автоматическое подключение locked packages к profile catalog.
- Добавлены scopes `global|corporate|project` с precedence `project > corporate > global > builtin`; governance всех уровней остаётся fail-closed. Lock фиксирует version/source/ref/Git commit/SHA-256 и verified signature metadata.
- `BlockPackage` получил dependencies, `requirements.takt` и adapter requirements `required|preferred`; required capabilities проверяются до Run, preferred availability передаётся Task Router/Planner.
- Добавлен `PackagePolicy`: source allowlist, trusted Ed25519 keys и обязательные подписи для выбранных scopes. Git sync восстанавливает exact commit; local sync запрещает drift от locked checksum/version.
- Закрыты замечания ревью v0.1.41: public agent-adapter envelopes/validators, shared fixtures, использование conformance kit реальным fake wrapper contract, явная граница OS exit-code проверки, настоящий `adapter doctor`, расширенные SDK tests, точная reconcile-документация и регрессии pause/cancel/overflow/boundary/evidence.
- Добавлены ADR-066/067, `docs/56-portable-package-distribution-v0.1.42.md`, package lock/policy/signature schemas и `scripts/test-package-distribution.sh`. Takt skill обновлён до 0.24.0.

## v0.1.41-alpha

- Добавлена Adapter Platform: публичный `sdk/domainadapter`, нейтральные `scm|tracker|ci` adapters, новый `adapter` Node action, capability preflight и `process|mcp` transports без платформенных имён в workflow.
- Process transport использует строгий `takt-domain-adapter/v1alpha1`; MCP transport выполняет discovery через `tools/list` и отображает нейтральные операции на конкретные tools. Добавлены CLI `takt adapter list|describe|doctor`, fake adapters и provider-neutral E2E.
- Side-effect reconciliation распространён на доменные adapters: capability проверяется до мутации, idempotency key/receipt сохраняются durable, `unknown` запрещает blind retry до reconcile.
- Добавлен `sdk/agentadapter` с conformance kit для сторонних `takt-assistant/v1alpha2` wrappers. Публичная agent MCP surface остаётся пять `takt.task.*` tools.
- Исправления ревью v0.1.40: спецификация `side_effect`, сохранение parking record при неудачном steering, достижимый `partial` verdict, Pi overflow timeout, единая parking-модель `BOUNDARY_VIOLATION`, evidence failed→passed regression, явные MCP surface counts и unknown-claim regression.
- Добавлены ADR-064/065, `docs/55-adapter-platform-v0.1.41.md`, `schemas/domain-adapter-protocol.schema.json`; Takt skill обновлён до 0.23.0.

## v0.1.40-alpha

- Добавлен внутренний `EvidenceManifest`: baseline provenance, stable failure fingerprints, check-to-evidence mapping и итоговый verdict, связанный с candidate content SHA-256. Изменение candidate после проверки делает verdict `stale`.
- Baseline-aware validation отличает исходные падения от новых regressions: exact normalized `BASELINE_FAILURE` сохраняется как evidence и не запускает automatic repair; новые failures идут по обычному `IMPLEMENTATION_FAILURE|VERIFICATION_FAILURE` routing.
- Dynamic Plan получил `parked` с компактным failure code, owner, `safe_next_action` и `unsafe_to_repeat`; Task API и attention показывают необходимость решения, а простой `continue` не обходит parking.
- External executor получил `side_effect.mode: idempotent|reconcile`, durable idempotency key/receipt/reconcile state и worker MCP `takt.node.reconcile`. Unknown outcome блокирует blind retry; `not_applied` разрешает новый claim; `applied` требует receipt и завершает node без повторения side effect.
- Полная MCP surface содержит 54 operations: agent 5, host 7, worker 13, operator 29. Agent surface не расширялась.
- Закрыты тестовые пробелы последнего ревью: lexeme matching, bounded repair branches, pause-before-attempt, реальный `question`, persistence errors, transient summary, plan-fork fingerprint, notification dispatch lock/prune/desktop timeout, `--file`, MCP default, direct-run capability preflight и router diagnostic/cancellation.
- Старые OpenCode timeout tests получили больший deadline для race-нагрузки; полный `go test -race ./internal/...` проходит единым прогоном.
- Добавлены `schemas/evidence-manifest.schema.json`, ADR-062/063 и release note `docs/54-evidence-baseline-failure-routing-v0.1.40.md`. `code-core` обновлён до 0.4.0, профиль `code` — до 0.15.0, Takt skill — до 0.22.0.

## v0.1.39-alpha

- Добавлены внутренние `RoleDefinition` и `TaskBrief`: trusted blocks получают функциональную роль, bounded context recipe, scope `expected|allowed|protected|forbidden` и fresh brief на каждую worker/repair-фазу без установки отдельных агентов в кодинг-хост.
- `BlockPackage` поддерживает structured checks с `required|preferred` и реакциями `deny|repair|warn`. Required `repair` получает одну автоматическую repair-итерацию и fresh recheck; повторный отказ переводит план в `waiting` с одним материальным вопросом.
- `code-core` получил роли baseline-observer/investigator/implementer/test-designer/validator/verifier; deterministic validation и independent review являются required+repair, adversarial review — preferred+warn. Package обновлён до 0.3.0, профиль `code` — до 0.14.0, Takt skill — до 0.21.0.
- Закрыты критичные pause defects v0.1.37: durable marker сохраняется через recovery, ошибочный resume не уничтожает pause, paused parent waiting child не оживает от ответа ребёнку, pause перепроверяется перед каждым sequential node и retry-attempt.
- Notification dispatcher получил реальный `question.required`, устойчивый dedup по waiting identity, sink timeout, межпроцессный dispatch lock, bounded inbox, baseline без ретроспективного спама и deterministic event IDs.
- Foreground recovery теперь исполняется до следующей durable границы; retry сохраняет per-attempt execution history и корректно работает из `cancelled`; recursive summary терпит ещё не опубликованного child; fork сохраняет source fingerprint/provenance; ошибки persistence operator markers не маскируются.
- Закрыты compact Task API defects v0.1.38: plan-level `answer` доставляется ожидающему Run/node, `stop` reconcile-ит plan без daemon, пустой answer отклоняется, отсутствующий router использует stable fallback, cancellation не поглощается, `task start` читает файл только через `--file`.
- `coding-agent` resolution сведён к `assistant.Factory.Resolve`, прямой `takt run` выполняет capability preflight, risk term matching больше не путает `auth/author` и `bug/debug`, пустой MCP surface означает `agent`.
- Добавлены `schemas/task-brief.schema.json`, расширенная schema `BlockPackage`, ADR-060/061, release note `docs/53-role-brief-controls-v0.1.39.md` и расширенный `scripts/test-simple-reliable-router.sh` с fail-once automatic repair.

## v0.1.38-alpha

- Добавлен Task Router с маршрутами `workflow|template|dynamic`, схемой `TaskRoute`, детерминированными risk signals и проверяемой компиляцией в один обычный Takt Workflow.
- Добавлен стабильный шаблон `simple-reliable`: investigate → implement → validate → review с прогрессивными controls baseline, independent tests, enhanced review, inspect checkpoint и max_parallel.
- Semantic router больше не является точкой недоступности: protocol/schema/runtime failure приводит к durable `router_fallback` и inspect-first template.
- Добавлен компактный пользовательский API `takt task start|status|respond|stop|explain` в CLI, daemon и MCP.
- MCP разделён на `agent|host|worker|operator|all`; основная LLM по умолчанию видит только пять `takt.task.*`, полная совместимая поверхность содержит 53 operations.
- Профиль `code` использует логический `coding-agent`; `default_assistant` выбирает Pi, OpenCode или внешний process adapter. Старый OpenCode config и конфигурация с одним assistant совместимы.
- Process config теперь принимает `takt-assistant/v1alpha2`; нейтральный `SessionAdapter` предназначен для Codex, Oh My Pi, Qwen CLI и других coding-agent wrappers без зависимости от Kiro CLI.
- В `code-core` добавлены trusted blocks `baseline` и `test-design`; версия профиля — 0.13.0, skill — 0.20.0.
- Добавлены proposal `docs/proposals/001-simple-reliable-agent-neutral-takt.md`, release note `docs/52-simple-reliable-agent-neutral-router-v0.1.38.md` и contract `scripts/test-simple-reliable-router.sh`.

## v0.1.37-alpha

- Добавлен Autonomous Run Operations: `runs`, `attention`, `run list|summary|watch|pause|resume|retry|fork|abandon|recover` и соответствующий daemon/MCP API.
- Safe pause прекращает запуск новых узлов и fan-out batches, каскадируется по governed child Runs и продолжает незавершённый граф после resume. Linked child ID получает marker даже до публикации собственного `state.json`.
- Operator retry сохраняет историю и сбрасывает failed node с зависимым хвостом; fork создаёт новый Run/Dynamic Plan; abandon является отдельным terminal-состоянием.
- Daemon выполняет PID-based recovery потерянных локальных executor: attempt получает `worker_lost`, node возвращается в `pending`, дети восстанавливаются раньше родителей.
- Добавлен notification dispatcher с durable inbox, дедупликацией, ack и sinks `coding_agent_host|desktop|process`; Pi/OpenCode получили команды runs/attention/pause/resume/result.
- Исправлен host-control: terminal session не переиспользуется, begin сериализуется file lock, повреждённые records fail-closed, транспортная ошибка bundled integrations сохраняет managed cache и блокирует обход.
- Pi 0.73.1 и OpenCode V2 integrations честно переведены в `guarded`: Pi не заявляет неподтверждённый completion gate, OpenCode помечен `verified:false`; убраны `before_agent_start`, floating `next` и fail-open steering/shell paths.
- MCP расширен до 48 tools; добавлены ADR-055/056, `docs/51-autonomous-run-operations-v0.1.37.md` и контракт `scripts/test-autonomous-runs.sh`.
- Takt обновлён до 0.1.37-alpha, skill — до 0.19.0; профиль `code` остаётся 0.12.0, поскольку его workflow-контракт не менялся.

## v0.1.36-alpha

- Добавлен Coding Agent Host Control: durable host sessions, CLI/MCP/daemon `host begin|confirm|get|find|guard|release` и строгие уровни `advisory|guarded|strict`.
- Добавлены экспериментальные host extensions Pi/OpenCode. Поздний аудит показал, что bundled adapters обеспечивают только guarded-контроль; это исправлено и явно зафиксировано в v0.1.37-alpha.
- Fingerprint `BlockPackage` расширен до транзитивного содержимого команд, вложенных workflow, script source/dependencies, path skills и MCP-конфигураций.
- Явный `allowed_integrations: []` теперь запрещает все интеграции; отсутствие поля не добавляет ограничения. Нулевая граница package limits документирована отдельно от bounded default динамического плана.
- Исправлены macOS unit expectation, единый revision limit steering для `running|waiting`, foreground locking, обработка ошибок workflow listing/JSON/promote rollback и точный atomic-block contract test.
- Добавлена миграционная заметка: статические профили без `block_packages` продолжают работать, Dynamic Plan требует явного каталога. `.commandcode/taste/taste.md` остаётся удалённым как мусор предыдущей разработки.
- Профиль `code` обновлён до 0.12.0, skill — до 0.18.0; MCP расширен до 36 tools; добавлены ADR-053/054 и `docs/50-coding-agent-host-control-v0.1.36.md`.

## v0.1.35-alpha

- Добавлен минимальный доверенный каталог `BlockPackage` для Dynamic Takt: встроенные, корпоративные и проектные блоки, типизированные выходы, возможности, интеграции, шаблоны и governance.
- Пакеты задают обязательные блоки/проверки, правила веток, шаблон change request, security policy и максимальные бюджеты; ограничения нескольких пакетов объединяются fail-closed.
- Профиль поддерживает `block_packages`; добавлены CLI `takt block list|describe|validate` и MCP `takt.block.list|describe`. Каталог и workflow блоков входят в fingerprint плана.
- `map` разрешает только точно объявленный массив результата; доверенный блок не может скрыто запускать governed child Runs. `adversarial-verify` получил отдельный workflow.
- Исправлен прямой `takt execute`: без daemon план выполняется до terminal/waiting, с `--daemon` остаётся отсоединённым. Добавлен межпроцессный lock продвижения планов.
- Лимиты revisions/child Runs/tokens теперь учитывают steering, planner, replanner, сегменты и детей; `max_parallel` ограничивает независимые task-фазы; `max_tokens: 0` нормализуется в bounded default.
- Исправлены macOS artifact paths с несуществующим файлом под symlinked prefix, полный анализ составных `when`, безопасный promote с `--force`, порядок применения steering и ошибки persistence.
- Профиль `code` обновлён до 0.11.0, coding-agent skill — до 0.17.0; добавлены ADR-051/052, schema/contract пакетов и `docs/49-trusted-block-packages-v0.1.35.md`.

## v0.1.34-alpha

- Добавлен Dynamic Takt: решение `existing|planned`, ограниченный `WorkflowPlan`, разрешённые workflow-блоки и компиляция в обычные governed child Run без второго runtime.
- Добавлены preview/confirmation, бюджеты child Runs/parallel/revisions/tokens, checkpoint replanning, immutable plan revisions и `takt steer`.
- Добавлены CLI `takt plan|get|promote`, `takt execute`, `takt steer` и MCP `takt.plan|get`, `takt.execute`, `takt.run.steer`, `takt.plan.promote`; MCP plane расширен до 27 tools.
- `takt plan get` показывает фазы, связанные Run, usage и артефакты; completed planned-план продвигается в `.takt/workflows/generated` после повторной проверки.
- Добавлен coding-agent skill flow: основная сессия Pi/OpenCode показывает preview и управляет Run, отдельные worker-сессии выполняют фазы.
- Исправлен daemon на длинных Unix socket paths macOS через hashed `$TMPDIR` fallback; terminal event subscription дочитывает журнал до revision состояния.
- External claim хранит `claimed_at`; устранена утечка process assistant v1alpha2 на protocol-ошибках; governed child input повторно валидируется после рендеринга.
- Профиль code выполняет `validation_commands` детерминированным `script.runtime: validation`, получил явные review/approval gates и непустые review inputs; `when` поддерживает `&&`/`||`.
- Профиль `code` обновлён до 0.10.0, authoring/coding-agent skill — до 0.16.0; добавлены ADR-049/050 и `docs/48-dynamic-takt-v0.1.34.md`.

## v0.1.33-alpha

- Добавлен authoring preflight: path-aware `did you mean`, capability validation в `takt validate`, статический анализ output/artifact references и `--warnings-as-errors`.
- Renderer стал fail-closed и поддерживает обязательные `${path}`, optional `${path?}` и default `${path:-value}`.
- Расширен проверяемый JSON Schema subset: min/max для массивов, строк, чисел и объектов, regex `pattern` и `description`.
- Добавлены `always_run` для cleanup/finally-узлов и activity-based `idle_timeout` для локальных и внешних AI-узлов.
- Добавлен локальный `takt daemon` на Unix socket и файловом Store: background Runs, event subscriptions, MCP proxy и несколько локальных клиентов без БД.
- Control plane сериализует concurrent mutations bounded retry, external task становится claimable только после durable suspension checkpoint, daemon shutdown ожидает завершения monitor goroutine.
- Authoring skill обновлён до 0.15.0; профиль `code` обновлён до 0.9.1 для явных optional recovery references.
- Добавлены ADR-047/048, contracts `test-authoring.sh`/`test-daemon.sh` и `docs/47-authoring-local-daemon-v0.1.33.md`.

## v0.1.32-alpha

- Завершён `takt-agent-events/v2`: session started/resumed, message, tool requested/allowed/denied/started/completed, artifact declared, usage, diagnostic и terminal events.
- Добавлен `takt-assistant/v1alpha2` с capability declaration, NDJSON stream и двунаправленным `tool.request`/`tool.decision`.
- Внешний executor поддерживает блокирующую policy/approval до запуска инструмента, отдельную отмену tool call и связь артефакта с `call_id`.
- MCP worker plane расширен до 22 инструментов. Узел нельзя завершить с незавершёнными tool calls.
- Шесть основных workflow профиля `code` получили строгие JSON-входы, специализированные фазы, обязательные checkpoint artifacts, domain error codes, Git decision trees и recovery.
- Добавлен сквозной Git/GitHub contract с настоящим локальным repository, bare remote, fake `gh`, успешным issue flow и validation-recovery flow.
- Профиль `code` обновлён до 0.9.0, authoring skill — до 0.14.0.
- Добавлены ADR-045/046 и `docs/46-controlled-agent-events-deep-workflows-v0.1.32.md`.

## v0.1.31-alpha

- Исправлен конкурентный read-path файлового store: transient state/events revision mismatch перечитывается с bounded backoff, persistent mismatch остаётся `InconsistentError`.
- Добавлен индекс `events.idx`; инкрементальный `run.events` читает журнал с нужного byte offset вместо полного сканирования.
- CLI lifecycle-команды делегируют общему `internal/control.Service`, устраняя дублирование approval, parent resume, children, artifacts и cancellation.
- JSON-RPC request IDs сохраняются без float64, envelope допускает extension fields и возвращает корректный `-32600` для invalid request.
- Добавлены нормализованные `assistant.*` и `tool.*` события для встроенных adapters и внешних исполнителей.
- Добавлен `executor: external` для `command`/`prompt`: durable pending task, capability claim, lease/token, normalized events, complete/fail и продолжение обычных output/retry/hooks/artifact semantics.
- MCP расширен инструментами `takt.node.pending|claim|event|complete|fail`.
- Fan-out по умолчанию отклоняет дубли, smart review требует непустой уникальный список, pre-start cancellation marker сохраняется.
- Static `subworkflow` корректно ребейзит script/dependencies/MCP/path skills; artifact type validation использует один общий контракт.
- Профиль `code` обновлён до 0.8.1: smart review требует непустой уникальный список перспектив.
- Authoring skill обновлён до 0.13.0; добавлен contract `scripts/test-external-executor.sh` и документ `docs/45-agent-events-external-executor-v0.1.31.md`.

## v0.1.30-alpha

- Добавлена команда `takt mcp` — локальный stdio MCP control plane поверх существующих runtime и файлового store.
- Поддержаны legacy `initialize` для версий 2025 и stateless `server/discover` для `2026-07-28`.
- Опубликованы 10 детерминированно упорядоченных tools для discovery, start/get/resume/answer/cancel, child Runs, artifacts и events.
- `takt.run.start` выполняется detached по умолчанию и возвращает durable `run_id`; `takt.run.events` поддерживает revision cursor и bounded long polling.
- Tool results одновременно содержат text content, `structuredContent`, `resultType: complete` и `isError`.
- Artifact tool поддерживает recursive filters и ограниченное UTF-8/base64 содержимое с сохранением checksum/provenance.
- Добавлены общий local control service, чтение events из store, MCP cancellation request contexts, unit/lifecycle tests и `scripts/test-mcp.sh`.
- Roadmap локального MCP перенесён в выполненные работы и заархивирован в `docs/44-local-mcp-control-plane-v0.1.30.md`.
- Authoring skill обновлён до 0.12.0 и дополнен инструкцией локального MCP.

## v0.1.29-alpha

- Добавлены `script`-узлы с runtime `command`, `python`, `node` и `go`, file/inline source, args, env, working directory и зависимостями.
- Исходники скриптов и явно объявленные зависимости входят в workflow fingerprint и блокируют небезопасный resume.
- `command`, `prompt`, `bash` и `script` поддерживают `output_type`, `output_mime` и `output_path`.
- Артефакты сохраняются как локальные снимки с SHA-256, размером, producer Run/Node и номером попытки.
- Добавлены шаблоны `${nodes.<id>.artifacts.<type>.<field>}` и CLI `takt artifacts`.
- Governed child Run и fan-out передают ссылки на типизированные артефакты родителю без потери provenance.
- Профиль `code` 0.8.0 использует script-узел для review perspectives и регистрирует plan/PRD артефакты.
- Authoring skill обновлён до 0.11.0; добавлен контракт `scripts/test-script-artifacts.sh`.

## v0.1.28-alpha

- Добавлен динамический `workflow.fan_out` из структурированного output upstream-узла.
- Каждый элемент получает отдельный governed child Run, устойчивый ID, состояние, события, артефакты и usage.
- Реализованы `max_parallel`, `all_success|all_done|one_success`, ordered aggregation, частичный resume и fingerprints списка.
- Добавлены множественное ожидание approval, выборочная отмена ребёнка и CLI metadata в `takt children`.
- Smart/comprehensive review профиля `code` 0.7.0 переведены на runtime fan-out.
- `scripts/test-worktree.sh` совместим со штатным Bash 3.2 macOS.
- OpenCode `read_only` теперь всегда запрещает `write`; merge `OPENCODE_CONFIG_CONTENT` стал рекурсивным.
- Зафиксировано различие skills: Pi поддерживает существующие path skills, OpenCode — path и named skills.

## v0.1.27-alpha

- Добавлены per-node `allowed_tools`, `denied_tools`, `skills`, `mcp`, assistant-enforced `sandbox` и `requires`.
- Явные пустые allowlists сохраняются как запрет инструментов/skills.
- Добавлена capability preflight до запуска adapter, persistence effective policy и наследование governed child Run.
- Process protocol и `TAKT_POLICY_JSON` передают policy внешнему adapter.
- Pi и OpenCode применяют tool/skill policies; OpenCode получает MCP config и path-skill instructions.
- Исправлена работа managed worktree через symlinked paths на macOS.
- Usage structural composition включает hidden nodes.
- `takt cancel` отклоняет все terminal Run.
- Governed recursion проверяется уже командой `takt validate`.
- Пустые worktree-ветки удаляются, ветки с коммитами сохраняются.
- Профиль `code` 0.6.0 применяет no-tool policy к роутерам и read-only tool restrictions к review agents.
- Добавлен контракт `scripts/test-policies.sh`.

## v0.1.26-alpha

- добавлен узел `workflow`, запускающий подключённый workflow как отдельный governed child Run со своим ID, state, events, artifacts, output и usage;
- RunState хранит `parent_run_id`, `parent_node_id`, `child_run_ids`, aggregate usage и durable cancellation state; узел хранит текущего ребёнка и историю child attempts;
- approval внутри дочернего Run можно отвечать через корневой Run; CLI продолжает ребёнка и каскадно возобновляет всю parent chain;
- добавлены `takt children` и `takt cancel`, включая durable marker, остановку активного процесса и каскадную отмену дерева;
- реализованы режимы изоляции ребёнка `inherit`, `worktree`, `none` и собственная policy дочернего workflow;
- retry узла `workflow` создаёт новый дочерний Run, сохраняя предыдущие попытки для аудита;
- fingerprint родителя рекурсивно включает definitions governed children; рекурсия и глубина 16 проверяются до запуска;
- умный router и reusable review-блоки профиля `code` переведены на отдельные child Runs;
- профиль `code` обновлён до 0.5.0, authoring skill — до 0.8.0;
- добавлены ADR-038, governed child run contract suite и `docs/40-governed-child-runs-v0.1.26.md`.

## v0.1.25-alpha

- добавлена управляемая Git worktree isolation: политика workflow, CLI-переопределения, отдельная ветка/каталог выполнения, сохранение состояния и безопасная очистка;
- умный роутер создаёт worktree только после выбора изменяющего дочернего workflow; direct selector применяет ту же политику при старте Run;
- добавлены `takt worktree list/remove/prune`, блокировка удаления активного Run и защита грязного worktree без `--force`;
- определения и команды fingerprint-ятся из control checkout, а CLI-решение по изоляции сохраняется для resume;
- исправлено сохранение raw stdout при `output_format`, добавлен protocol retry с точным `${feedback}`, устранён двойной retry роутера;
- исправлены approved-итерация `interactive-prd`, fallback `create-issue`, публикация параллельного статуса и exact integer validation;
- полный review block использует настоящий `foreach.parallel` по пяти перспективам;
- профиль `code` обновлён до 0.4.0, authoring skill — до 0.7.0;
- добавлены ADR-037, worktree contract suite и `docs/39-git-worktree-isolation-v0.1.25.md`.

## v0.1.24-alpha

- профиль `code` 0.3.0 получил полный каталог из 19 процессов разработки и умный роутер в обычном Run;
- Profile поддерживает именованные `workflows`, селектор `profile:workflow`, `takt workflow list` и `takt workflow describe`;
- `command` и `prompt` поддерживают проверяемый `output_format`, а шаблоны и `when` — вложенные JSON-пути;
- scheduler выполняет независимые простые узлы параллельными волнами с сериализованным persistence и моделью all-settled;
- `foreach.parallel` выполняет итерации конкурентно и сохраняет порядок входного массива в агрегированном output;
- approval разрешён внутри `loop_group`; resume продолжает активную итерацию, а следующая итерация создаёт новый approval;
- добавлено `trigger_rule: one_success` для соединения условных ветвей;
- добавлены reusable full/smart review blocks, тест роутера, race/timing regressions и контракт всех 19 workflow;
- authoring skill обновлён до 0.6.0;
- добавлены ADR-035, ADR-036 и `docs/38-archon-workflow-catalog-v0.1.24.md`.

## v0.1.23-alpha

- `foreach` поддерживает внешний YAML/JSON-массив через `items_from.path`; содержимое входит в workflow fingerprint;
- `subworkflow` и `foreach` разрешены внутри `loop_group` без второго scheduler;
- output `foreach` стал упорядоченным JSON-массивом результатов всех итераций;
- CLI возвращает публичное состояние Run без внутренних развёрнутых ID и принимает approval по ID контейнера;
- контейнер поддерживает defaults `assistant`, `model` и `session`;
- локальные команды подключённого workflow ищутся до корня композиции, включая корневой `commands/` профиля;
- схема согласована с value-семантикой контейнера, задокументированы рекурсия и предел глубины 16;
- усилены timeout/overflow-регрессии, устранена гонка закрытия stderr в Pi adapter, расширены тесты композиции и исправлена проверка документации;
- профиль `code` обновлён до 0.2.1, authoring skill — до 0.5.0;
- добавлены ADR-034 и `docs/37-composition-hardening-v0.1.23.md`.

## v0.1.22-alpha

- добавлены reusable `subworkflow` с inputs, автоматическим или явным `output_node` и публичным output;
- добавлен последовательный `foreach` по явным scalar/JSON-object items;
- композиция компилируется в обычный DAG с единым scheduler и сохраняемыми approval/retry/status;
- подключённые workflow и локальные команды входят в workflow fingerprint;
- профиль `code` 0.2.0 разделён на переиспользуемые фазы implementation и review;
- authoring skill обновлён до 0.4.0;
- добавлены composition example, contract suite, ADR-033 и `docs/36-workflow-composition-v0.1.22.md`.

## v0.1.20-alpha

- OpenCode сохраняет сообщения о provider retry и connection failure при timeout/cancellation;
- raw stdout/stderr и краткая диагностика попадают в NodeState и per-attempt execution record без изменения execution kind;
- scheduler сохраняет специализированную context-ошибку adapter вместо общего `node attempt`;
- authoring skill обновлён до v0.2.1, а его README и поддерживаемая версия Takt проверяются автоматически;
- добавлены ADR-031 и `docs/34-opencode-provider-diagnostics-v0.1.20.md`.

## v0.1.19-alpha

- добавлен специализированный `type: opencode` через `opencode run --format json`;
- поддержаны model/agent/variant, fresh/resume, version probe, per-step usage/cost и error events;
- добавлены fake OpenCode binary, contract suite, runtime retry/resume test и opt-in real smoke;
- OpenCode включён в config schema, примеры и Takt authoring skill v0.2.0;
- добавлены ADR-030 и `docs/33-opencode-adapter-v0.1.19.md`.

## v0.1.18-alpha

- добавлен canonical skill `skills/takt/SKILL.md` для создания, изменения, проверки и запуска Takt-профилей;
- справка скилла разделена на configuration, workflows, patterns и troubleshooting;
- добавлен копируемый `validated-agent-profile` с inline prompt, моделями на узлах, Markdown-командой, validator retry/resume и approval;
- добавлен контрактный `scripts/test-takt-skill.sh`, который проверяет структуру скилла и валидирует оба шаблонных workflow;
- прежний `examples/coding-agent-skill` переименован по назначению в минимальный `takt-runner`;
- добавлен документ `docs/32-takt-authoring-skill-v0.1.18.md`.

## v0.1.17-alpha

- добавлен корневой `AGENTS.md` с краткими правилами работы кодовых агентов;
- bash runtime сохраняет stdout и stderr отдельно, сохраняя объединённый `output` для feedback и диагностики;
- evaluation декодирует `takt-validation/v1alpha1` только из stdout quality-node;
- stderr валидатора больше не повреждает корректный validation envelope и сохраняется в отчёте отдельно;
- добавлены регрессии `valid:false + stderr + exit 1`, схема состояния и схема evaluation report;
- добавлены ADR-029 и документ `docs/31-quality-stdout-separation-v0.1.17.md`.

## v0.1.16-alpha

- quality envelope декодируется и сохраняется независимо от exit code и terminal status quality-node;
- `score`, `checks` и diagnostics из `valid: false` с ненулевым exit code участвуют в предметных агрегатах;
- успех benchmark определяется только сочетанием `quality_node_status: completed` и `quality.valid: true`;
- `valid: true` из failed/errored/timed_out/cancelled узла сохраняется для аудита, но не повышает success rate;
- malformed validation envelope при любом статусе остаётся ошибкой измерительного контура;
- evaluation report сохраняет `quality_node_status`;
- добавлены ADR-028 и документ `docs/30-quality-envelope-semantics-v0.1.16.md`.

## v0.1.15-alpha

- quality summary сохраняет измеренные нули как `0`, а недоступные средние значения как `null`;
- `NodeState.executions` сохраняет assistant/version/requested/resolved model и usage каждой фактической попытки;
- evaluation report помечает mixed execution identity и группирует tokens/cost по отдельным identity;
- JSON с `valid: true` учитывается только от quality-node со статусом `completed`;
- benchmark fingerprint включает ID и объявленную версию валидатора;
- `duration_per_valid_ms` заменён на точное по смыслу `amortized_end_to_end_ms_per_valid`;
- opt-in Pi smoke проверяет наличие фактического `ResolvedModel`;
- добавлены ADR-027 и отчёт `docs/29-benchmark-metric-semantics-v0.1.15.md`.

## v0.1.14-alpha

- evaluation report получил формат `takt-evaluation/v1alpha1`;
- добавлены `strategy_id` и fingerprints workflow/config/Markdown-команд;
- добавлены benchmark/dataset/workspace/validator fingerprints и версия валидатора;
- `NodeState` и report сохраняют assistant, его версию, requested model и фактический Pi `responseModel`;
- добавлен строгий предметно-независимый контракт `takt-validation/v1alpha1`;
- summary рассчитывает success@1, final success, average score, attempts/cost/time per valid и diagnostics по severity/code;
- `examples/route-dsl-eval` закреплён как инфраструктурный suite, добавлен отдельный `examples/route-dsl-benchmark` для реального Pi и штатного валидатора;
- добавлены схемы validation result/evaluation report, ADR-026 и отчёт `docs/28-benchmark-identity-quality-v0.1.14.md`.

## v0.1.13-alpha

- evaluation preflight отклоняет коллизии нормализованных `case_id` до создания output;
- `workspace-template` и `output` не могут совпадать или быть вложены друг в друга, включая разрешение символических ссылок;
- `NodeState` сохраняет подтверждённый факт resume;
- `report.json` содержит `resumed`, `feedback`, ошибку и диагностический вывод каждого узла;
- Route DSL eval suite проверяет retry/resume и сохранение validator diagnostics;
- добавлены ADR-025 и отчёт `docs/27-evaluation-isolation-report-v0.1.13.md`.

## v0.1.12-alpha

- Route DSL E2E больше не зависит от команды `python`; JSON-проверки выполняются Go helper-ом;
- интеграционные timeout/cancel + overflow тесты используют корректные `context.WithTimeout` и `context.WithCancel`;
- runtime scheduler проверяет сохранение `output_truncated` в итоговом `NodeState`;
- `NodeState.usage` накапливает tokens и cost всех агентных попыток;
- добавлены `takt eval run` и `takt eval report` для изолированного прогона каталогов заданий;
- evaluation report содержит статусы, attempts, duration, usage, approvals и truncation;
- добавлен стартовый набор из десяти Route DSL заданий и контрактный eval-тест;
- добавлен отчёт `docs/26-evaluation-runner-v0.1.12.md`.

## v0.1.11-alpha

- добавлены fake-Pi overflow-сценарии, проходящие через реальный `Pi.Run`;
- интеграционно проверены `timed_out`/`cancelled` и `Result.Truncated=true`;
- runtime-регрессия подтверждает сохранение `NodeState.OutputTruncated` для context errors;
- добавлен воспроизводимый Route DSL end-to-end: Pi → validator → feedback → retry/resume → artifacts → approval;
- первая попытка намеренно не проходит проверку, вторая использует Session ID и диагностику;
- новый сквозной сценарий включён в `make check` и `scripts/verify.sh`;
- добавлены ADR-023 и отчёт `docs/25-route-dsl-e2e-v0.1.11.md`.

## v0.1.10-alpha

- timeout/cancellation Pi attempt имеют приоритет над одновременно обнаруженным output overflow;
- `output_truncated` сохраняется как диагностика без изменения `timed_out`/`cancelled`;
- исчезновение cumulative usage после его наличия в первом снимке классифицируется как `protocol`;
- явные нулевые usage-значения остаются валидными;
- добавлены регрессии timeout+overflow, cancel+overflow, missing usage и zero usage;
- добавлены ADR-022 и отчёт `docs/24-pi-context-usage-hardening-v0.1.10.md`.

## v0.1.9-alpha

- Pi adapter завершает попытку только после `agent_settled`, а не первого `agent_end`;
- fake Pi моделирует automatic retry и проверяет, что Takt не возвращает частичный результат;
- расширен deny-list session/mode CLI-флагов Pi, включая короткие aliases;
- fire-and-forget `set_editor_text` допускается без ответа;
- usage Pi вычисляется как дельта накопленной статистики до/после prompt;
- добавлены регрессии для fresh/resume usage delta и уменьшения cumulative stats;
- добавлен ADR-021 и отчёт `docs/23-pi-rpc-alignment-v0.1.9.md`.

## v0.1.8-alpha

- добавлен специализированный `type: pi` через официальный `pi --mode rpc`;
- реализованы provider/model/thinking mapping, version probe и project trust;
- реализованы проверенные `fresh` и `resume` по фактическому Session ID;
- нормализованы итоговый текст, usage, resolved model, stdout/stderr и structured metadata;
- добавлены timeout/cancel, общий race-safe output limit и process-group termination;
- добавлены `cmd/takt-fake-pi`, Pi contract suite, runtime retry/resume test и opt-in real smoke;
- Pi-specific Config и JSON Schema синхронизированы;
- закрыты P2 документации: нумерация adapter tests, актуальная runtime version и optional metadata policy;
- добавлен ADR-020 и отчёт `docs/22-pi-adapter-v0.1.8.md`.

## v0.1.7-alpha

- OS exit code и `result.exit_code` в `takt-assistant/v1alpha1` обязаны совпадать всегда, включая ноль;
- добавлены отрицательные contract cases для версии, type, неизвестных полей/status, отсутствующего/null `exit_code`, несовместимых status/exit, двух JSON-значений и OS/envelope mismatch;
- decoder проверяет неотрицательные `usage.input_tokens`, `usage.output_tokens` и `usage.cost`;
- fake assistant отклоняет второй JSON request envelope;
- contract suite проверяет передачу `metadata` и `native_hooks`;
- `config.schema.json` запрещает `protocol` для `type: mock`, как runtime validator;
- добавлен отчёт `docs/21-protocol-hardening-v0.1.7.md`.

## v0.1.6-alpha

- реализован JSON-протокол `takt-assistant/v1alpha1` для process assistant;
- добавлен `cmd/takt-fake-assistant`;
- добавлены contract cases success, exit, start, timeout, cancel, concurrent output, malformed result, fresh, resume, resume rejection и output limit;
- runtime передаёт Run ID, Node ID и номер попытки в adapter;
- session resume проверен сквозным retry-тестом runtime;
- fake-assistant suite включён в `scripts/verify.sh`;
- обновлены JSON Schema, спецификации, backlog и документация.

## v0.1.5-alpha

- восстановлены полные редакции документов `v0.1.2–v0.1.3`, случайно перезаписанные при сборке `v0.1.4`;
- поверх восстановленной документации перенесена семантика parent-loop timeout/cancel из `v0.1.4`;
- восстановлены ADR-008–ADR-016, актуальные runtime specification, adapter contract, document map и coding-agent guide;
- добавлен отчёт `docs/19-document-recovery-v0.1.5.md`;
- кодовая семантика относительно `v0.1.4-alpha` не изменена.

## v0.1.4-alpha

- timeout и cancellation родительского `loop_group` сохраняют классификацию `timed_out`/`cancelled`;
- ошибка истёкшего attempt context имеет приоритет над производной ошибкой контейнера, включая `loop_group exhausted`;
- код ошибки Run для внешней отмены и deadline больше не записывается как `internal`;
- добавлены регрессии timeout и cancellation родительского `loop_group`;
- документация и результаты проверок обновлены перед fake-assistant contract suite.

## v0.1.3-alpha

- общий лимит stdout/stderr process assistant стал thread-safe;
- добавлен race-регрессионный тест одновременного stdout/stderr;
- `node.timeout` теперь ограничивает всю попытку: `before_node`, действие, `on_failure`, `after_node`, `before_complete`;
- timeout и cancellation внутри hooks сохраняют статусы `timed_out` и `cancelled`;
- вложенные `loop_group` явно запрещены в `v1alpha1` валидатором, JSON Schema и runtime-защитой;
- runtime предотвращает перезапись существующего состояния дочерним узлом цикла;
- `until` считается выполненным только для child node со статусом `completed`;
- добавлены регрессии для hook timeout/cancel, nested loops и skipped until-node;
- документация и схемы синхронизированы с фактической семантикой.

## v0.1.2-alpha

- исправлена семантика `allow_failure`: разрешается только ненулевой exit code;
- добавлена классификация `exit/start/timed_out/cancelled/protocol/internal`;
- добавлены Node statuses `errored`, `timed_out`, `blocked`;
- scheduler продолжает DAG после failure и выполняет `all_done`;
- root DAG и `loop_group` используют один scheduler;
- `when` и `trigger_rule` работают внутри `loop_group`;
- добавлены node timeout, process output limit и `output_truncated`;
- на Unix cancellation завершает process group;
- добавлены fingerprints workflow/config/commands;
- `answer` проверяет определения до сохранения ответа;
- добавлены lock Run и команда `takt resume`;
- persistence использует обязательные revisions state/event и обнаруживает рассогласование;
- YAML parser сохраняет пустые строки и поддерживает chomp modes block scalar;
- CLI использует единый JSON success/error envelope;
- `command run` использует user command scope;
- добавлены contract tests отказов, persistence, YAML, adapter и CLI;
- зафиксирован trusted local scope текущей версии.

## v0.1.1-alpha

- добавлено целевое состояние Takt v0.2;
- добавлена спецификация runtime-семантики;
- добавлен целевой контракт Pi/OpenCode/process adapters;
- добавлены план реализации, backlog и инструкция для кодового агента;
- добавлены JSON Schemas текущего `takt/v1alpha1`;
- добавлена карта документов и источников истины;
- process adapter выставляет переменные `TAKT_*`;
- версия CLI обновлена до `v0.1.1-alpha`.

## v0.1.0-alpha

- базовый Go-runtime;
- Markdown-команды, модели и process/mock assistants;
- DAG, hooks, retries, loop_group и approval pause/resume;
- локальное состояние и журнал событий;
- примеры Route DSL и hook retry.

## v0.1.21-alpha

- Добавлены пакеты профилей и команды `takt init <profile>`, `takt validate <profile>`, `takt run <profile>`.
- Добавлен встроенный профиль `code` для реализации Markdown-плана без обязательного task AST.
- Добавлена схема `schemas/profile.schema.json` и контрактный тест `scripts/test-code-profile.sh`.
- Authoring skill обновлён до 0.3.0 и обучен использовать готовые профили.
