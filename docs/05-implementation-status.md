# Текущее состояние реализации

Статус после `v0.1.63-alpha`. Документ описывает фактическое состояние, а не исторический backlog.

## Unified Run evaluation — реализовано в v0.1.63

- ordinary evaluation является одним root Run с последовательными
  `matrix` branches для case/repeat и произвольным authored DAG без встроенных
  стадий candidate/validator;
- только обычный `workflow` action создаёт governed child Run; dynamic
  `workflow.path`/`repository` проходят containment/fingerprint preflight до
  первой branch action;
- `RunState.result_revision` pin-ит terminal result независимо от последующих
  administrative commits и очищается перед operator retry;
- immutable `takt-assessment/v1alpha1` artifact связывает deterministic
  validation result и evidence с точной result revision target; stale и corrupt
  записи вычисляются/read fail-closed без изменения target;
- `valid:false` остаётся completed measurement, а malformed result, missing
  evidence и persistence failure делают evaluation Run failed;
- `script.stdin`, root JSON `$INPUTS.*` и `$MATRIX.item|index|total` образуют
  общий authoring boundary, не evaluation-specific callback;
- `takt-evaluation-input/v1alpha1` материализуется preflight-ом; новый path
  запускает workflow один раз и не пишет legacy report/progress как source of
  truth;
- canonical `run status|stats|inspect|assessment` работают по любому Run ID;
  gates вычисляются после durable reload и меняют только CLI exit;
- mini-du и Make live targets переведены на authored
  `workflows/evaluate.yaml` и Run ID. Exact
  `takt-flow-evaluation/v1alpha1` suites и directory readers сохранены как
  deprecated read/run compatibility; `eval flow init` пока остаётся legacy
  scaffold.

## Assistant configuration fail-fast — реализовано в v0.1.62

- Pi startup `Unknown provider` классифицируется как terminal
  `configuration`, сохраняет stderr и сообщает запрошенные provider/model с
  подсказкой `pi --list-models`;
- configuration failure не попадает под workflow/provider retry;
- flow evaluation записывает его как `infrastructure_error`, пропускает
  предметный validator, сохраняет evidence/cleanup и не запускает оставшиеся
  cases с тем же нерабочим config.

## Test suite tiers — реализовано в v0.1.61

- `make test`/`make race` проверяют core Go packages без `tests/e2e`;
- `make test-all`/`make race-all` явно запускают весь package suite;
- `make check` компилирует все Go-пакеты, запускает короткие architecture/profile/parser
  contracts, build и TypeScript smoke; process-heavy E2E не входит в этот
  ежедневный шлюз;
- `make check-full` и `scripts/verify.sh` сохраняют полный обычный/race
  release contour, а `make journeys` остаётся отдельным user-facing gate;
- независимые E2E-контракты и table-driven cases запускаются параллельно через
  `GO_TEST_PARALLEL_P` (по умолчанию 16); дорогой benchmark E2E оставляет один
  representative case/repeat, а полная агрегация и gate-failure semantics
  проверяются unit-контрактами;
- live `eval-*` цели не входят в автоматические проверки.

## Flow-evidence stabilization — реализовано в v0.1.63

- product `source/` публикуется атомарно; symlink/non-regular entry сохраняет
  `source-unavailable.txt`, не маскируя измеренный validator outcome
  инфраструктурной ошибкой;
- binary secret и ошибки persistence по-прежнему fail-closed;
- relative Pi session evidence безопасно разрешается внутри execution workspace;
- профиль `code` 0.19.4 ограничивает validation/revalidation probes, требует
  прямого сравнения с заявленным внешним эталоном, выносит scratch data из
  execution workspace и принимает единственный case-insensitive verdict как
  обычную строку или Markdown-заголовок;
- mini-du validator 4 проверяет продукт до отсутствующих delivery artifacts,
  не понижая fail-closed приоритет ошибок artifact inspection.

## Feature-flow gate matrix tiering — реализовано в v0.1.61

- `require-artifacts` и `require-verdict` являются общими executable tools
  профиля `code`; `feature-development` сохраняет прежние node/branch
  semantics и вызывает их из YAML;
- полные формы artifact и verdict parser cases проверяются в
  `internal/profile`, а full-flow E2E оставляет representative scheduler и
  downstream-gate сценарии;
- обычный `tests/e2e` после переноса матриц сократился с `290.661s` до
  `182.723s` внутри финального `make check`, а весь быстрый gate — до
  `267.39s`.

## Evaluation progress timings — реализовано в v0.1.60

- `progress.json` сохраняет фазовые тайминги `prepare`, `validator_preflight`,
  `workflow`, `validator`, `evidence` и `cleanup`;
- live assistant timing сохраняет наблюдаемые Pi wait/stream/total и
  завершённые tool intervals; `eval status` показывает их без обращения к
  модели;
- повторяющиеся cumulative Pi completion events одного model call не
  удваивают `LLM total`/`LLM stream`;
- старые snapshots без timing object остаются читаемыми и явно показывают
  недоступность, а измеренный ноль сохраняется как `0`.

## Provider availability recovery — реализовано

- adapters классифицируют доказанные transient provider outages как
  `provider_unavailable`; runtime сохраняет отдельные provider attempts,
  session-preserving retries (`2s`/`4s`, `Retry-After <= 60s`) и lifecycle
  events `provider.retry.scheduled|ready|exhausted`;
- Явный ответ Pi/OpenCode `Connection error` классифицируется как transient
  transport outage, а не как обычный assistant `exit`.
- предел — три Takt adapter calls на workflow attempt, без учёта Pi/OpenCode
  internal retries и без расхода workflow retry budget; recovery сохраняет
  backoff/session/исходный node deadline и не превращает in-flight retry в
  `worker_lost`; provider backoff и resume входят в один `node.timeout`;
- flow evaluation сохраняет diagnostics/usage, но классифицирует exhaustion как
  infrastructure error и исключает его из quality denominators. Domain side
  effects не используют provider retry.
- Pi model output exhaustion (`stopReason: length`) fail-closed как обычный
  `exit`; `code:feature-development` делает до трёх exact-session попыток вместо
  ложного `implement: completed` без результата.
- `code:feature-development` после assistant `create-pr` выполняет eval-only
  deterministic `pr-effect-gate`: при наличии SCM fixture он требует запись
  `pr create` в `calls.log` и не запускает `summary` при отсутствии side effect.

## Shared model presets — реализовано

- Config принимает строгие `model_preset` и `model_presets` с произвольными
  aliases и атомарными `provider/model-id`; `models` и preset mode взаимоисключающие.
- `takt run`, `takt validate`, `takt command run`, daemon/MCP `run.start` и eval
  используют один materializer; выбор сохраняется в durable Run options.
- Flow reports содержат выбранный preset и effective model references; только
  materialized Config входит в strategy fingerprint.

## Archon A1 review remediation — реализовано

- durable redaction покрывает `cancel_reason` и `loop_iterations[].until_bash`;
- shell/template surfaces используют quote-state, durable `BASE_BRANCH` и
  отдельную argv/env границу для scripts;
- loop predicate errors и failure-like body nodes сохраняют snapshot и делают
  safe-stop; shared sessions выбирают ближайшего ancestor и отклоняют
  конкурентное использование;
- schema разрешает `cancel`/`context: shared` только в loop body, а
  текущая документация проверяется inventory-тестом.
- shell lexer fail-closed учитывает комментарии, heredoc и вложенные
  command substitutions; root `fresh_context`/`until_bash` отклоняются,
  `fresh_context` переопределяет shared session только на первой попытке
  итерации `N > 1`, а retry session определяется `attempts.retry_session`; case
  patterns и expanded loop paths не дублируют parent namespace.

## Outcome-gated Development Flow Acceptance — реализовано в v0.1.59

- профиль `code` 0.19.4 требует user-owned `allowed_paths` для `plan-to-pr`;
- native Git pathspec scope gate проверяет actual tracked/untracked state до draft PR и после review fixes;
- PR, review и summary domain results закрыты workflow-level gates без новой runtime-абстракции;
- `code:feature-development` принимает единственный нормализуемый `validation.md` verdict: keyword/value case-insensitive, необязательный ATX Markdown heading; `PASS` продолжает flow, `REPAIR` разрешает ровно одну repair и независимую revalidation, `BLOCKED` делает safe stop;
- initial/revalidation parsers fail-closed на missing, duplicate, malformed и binary-tainted control lines; producer assistant обязан быть `completed`;
- acceptance gate сохраняет initial review evidence, требует review-fixes/revalidation artifacts после repair и не маскирует failure downstream узлов;
- evaluation SCM fixture требует хотя бы один успешный `pr create` receipt (`calls.log` + `pr-count`); `fake-gh` поддерживает проверку открытых PR через `pr list --head/--state` и сохраняет созданные записи; production SCM runs не изменены;
- authoring/schema contract fail-closed проверяет `hook.on_failure.session`: только `fresh|resume` и только при `action: retry`;
- Go E2E классифицирует happy path как `safe_success`, а missing artifact, false validation, blocked implementation, scope drift, blocked PR, unresolved review и incomplete summary — как `safe_stop`;
- remote PR receipt/reconcile по-прежнему не заявляется: E2E сверяет assistant evidence с fake SCM.

## Post-audit repair

- clean-checkout module graph восстановлен через `go mod tidy`;
- YAML/JSON authoring принимает ровно один документ, а malformed quoted `when` отклоняется до Run;
- GitHub Actions устанавливает TypeScript 5.7.2 и требует host-integration compiler smoke; локальный Go-only gate сохраняет явный `SKIP`, если compiler не установлен.

## Architecture contracts — выполнено в v0.1.57

- конституция языка workflow закрепляет границу «YAML координирует; код вычисляет; агент принимает решения» и запрет incremental expression creep;
- `when` имеет одну реализацию `internal/whenexpr` и проверяется loader-ом до Run;
- bundled assistant providers подключаются через immutable `ProviderRegistration` registry, собираемый только в bootstrap;
- canonical appapi/MCP/docs contract описывается одним schema-first `OperationDescriptor`;
- generated `docs/71-canonical-operation-contracts.generated.md` и architecture tests блокируют drift.

## Codebase hygiene — выполнено в v0.1.56

- JSON Schema instance validation делегирован `jsonschema/v6`; `schemasubset` хранит только публичную subset policy.
- Pi/OpenCode реализации отделены от stable assistant core и подключаются как extensions через bootstrap.
- test fixture binaries вынесены из product `cmd/`.
- process v1alpha2, script execution и governed child Run декомпозированы без изменения контрактов.
- CLI/MCP/compatibility surface явно отличают experimental Dynamic Flow/Host Control от stable core.

## Модульная стабильность — реализовано в v0.1.55

Функциональность сохранена, но разделена по стабильности и назначению:

- stable application/runtime/workflow/config/profile/store не зависят от experimental/tooling/extensions;
- Dynamic Flow, evidence routing, Host Control и Learning Loop находятся в `internal/experimental`;
- Package Distribution, Block Catalog и Notifications — в `internal/extensions`;
- evaluation и compatibility — в `internal/tooling`;
- `internal/application` уменьшен примерно с 6,8 тыс. до 2,2 тыс. production-строк без удаления use cases; внешний worker/tool lifecycle выделен в `internal/externalworker`, а общие durable lock/redaction helpers — в `internal/runcontrol`;
- самописный YAML parser удалён; YAML syntax делегирован `go.yaml.in/yaml/v3`, Takt сохраняет только strict JSON-shaped contract adapter;
- `make journeys` отдельно проверяет четыре стабильных пользовательских сценария через настоящий CLI.

Dynamic Flow остаётся доступным и regression-tested, но считается experimental до production evidence.

## Ядро runtime — реализовано

- один DAG scheduler для root/composition/dynamic compiled workflows;
- dependencies, `when`, trigger rules, `always_run`, retries/backoff, hooks, timeout/cancel;
- sequential/parallel `foreach`, `loop_group`, `subworkflow`, governed child `workflow`;
- first-class durable `loop_iterations[]` для всех завершённых итераций `loop_group`, совместимый `loop_previous`, bounded `max_iterations <= 64`;
- parallel waves и governed fan-out с `all_done|all_success|one_success` short-circuit;
- durable Run state/events/revisions, resume, pause, abandon, recovery и child lifecycle;
- canonical `NodeState.path`, diagnostic fingerprints и durable retry deadline;
- managed Git worktree, repository catalog и multi-repo governed child Runs;
- script/command/assistant/adapter/approval actions и typed artifacts.

## Coding-agent integration — реализовано, live strict conformance ограничено

- process protocol `takt-assistant/v1alpha1|v1alpha2`;
- Pi и OpenCode adapters;
- external executor и controlled tool lifecycle;
- `sdk/agentadapter` conformance kit;
- logical `coding-agent` + `default_assistant`;
- host-control core и Pi/OpenCode integrations;
- MCP surfaces agent/host/worker/operator.

Live smoke на Qwen 3.6 27B подтвердил adapter fresh/exact resume для Pi `0.83.0` и OpenCode `1.18.14`. Pi extension load и `/takt` command interception подтверждены; OpenCode plugin load, command/input interception и fail-closed durable recovery подтверждены. Найденные incompatibility/argv/diagnostic/tool-deny defects устранены; точный `output_format` теперь передаётся assistant, после чего оба live router вернули валидный `TaskRoute` с первой попытки без fallback. Bundled integrations остаются `guarded`: Pi input/tool/recovery/completion и OpenCode tool/completion не имеют live-доказательства, `strict` не заявляется.

## Dynamic Takt — реализовано

- Task Router `workflow|template|dynamic`;
- foreground и maintenance advancement ждут первый durable state accepted
  detached Run и не превращают его краткую недоступность в plan failure;
- `simple-reliable` template с monotonic controls;
- bounded `WorkflowPlan` из trusted blocks;
- preview/confirmation, budgets, checkpoints, replan, steering, immutable revisions;
- Role Contract, bounded TaskBrief и scope/check policies;
- EvidenceManifest, baseline classification, parking и repair routing;
- promotion успешного динамического плана в project workflow.

## Интеграции и доставка — реализованы как платформенные contracts

- neutral SCM/Tracker/CI domain operations;
- process/MCP transports;
- capability discovery;
- durable idempotency/receipt/reconciliation;
- portable BlockPackage install/update/sync/sign, lock/dependencies/scopes/source/signature policy;
- Agent Adapter SDK.

Provider-specific adapters не встроены в runtime. `v0.1.49` добавляет две reference implementations поверх public SDK: Qwen Code process wrapper и GitHub SCM/`gh` adapter. Fake fixtures остаются deterministic release gate; live credentials/providers требуют отдельного smoke. Корпоративные implementations должны использовать те же public contracts.

## Локальная эксплуатация — реализована

- stdio MCP и локальный daemon через Unix socket;
- background Runs и event subscriptions;
- run registry/attention/result summary;
- pause/resume/retry/fork/abandon/recovery;
- local notification sinks;
- local single-user trust model.

## Security/reliability — реализовано для локальной trusted модели

- `secret://ENV_NAME` и fail-closed resolution;
- общий persistence redaction runtime + control/external paths;
- redaction approval, external tool I/O/results, domain receipts и textual artifacts;
- binary artifact с известным secret блокируется;
- templated SecretRef регистрируется после render;
- local OS sandbox для `bash/script/validation/hooks`: bwrap Linux, sandbox-exec macOS, `required|optional`;
- assistant sandbox остаётся capability contract конкретного coding-agent;
- external mutating side effects используют idempotency/reconciliation.

Это не multi-user security boundary. Vault/RBAC/container orchestration и untrusted workflow execution не заявляются.

## Evaluation — реализовано

### Ordinary Run evaluation — v0.1.63

Один root Run материализует cases/repeats как ordered `matrix` branches.
Authored workflow владеет preparation, candidate child workflows, validation,
evidence и primary/advisory assessments. Run state/events/artifacts являются
source of truth; общие Run queries строят status, stats, deterministic inspect,
assessment relations и gates из durable Store без запуска модели.

### Workflow-level

`eval run/report/benchmark/compare`:

- EvaluationMatrix/CaseManifest;
- repeat и pairwise outcomes;
- strategy/benchmark/validator/workspace fingerprints;
- success@1/final success/attempts/score/cost;
- true time-to-valid из durable events;
- failed-execution cost;
- diagnostic fingerprints и stable/unstable cases;
- category breakdown и regression gates.

### Task-level — v0.1.46

`eval task-benchmark` запускает настоящий control path и измеряет:

- route accuracy;
- final success;
- plan revisions;
- replanner/execution Runs;
- replan expectation;
- unexpected needs-input;
- router fallback;
- aggregate usage;
- pairwise baseline/candidate transitions и gates.

Deterministic fixture доказывает measurement correctness. Production quality требует реальных cases/models.

Первый live Go-срез на пяти production-shaped cases выполнен с моделями Qwen. Pi/Qwen 3.6 `repeat=3` дал direct `14/15` и repair `14/15 → 15/15` с одним exact resume. Текущий OpenCode/Qwen3-Coder-Next дал direct `15/15` и repair `13/15 → 15/15`: два `GOFMT_FAILED` восстановлены exact resume, failed executions отсутствуют, все cases stable-valid. Сохранённый OpenCode/Qwen 3.6 evidence заметно хуже: прямой `aihub-sbt` дал `0/15 → 0/15`, `aihub-proxy` до исправления SSE — `0/15 → 6/15`, а post-fix smoke без transport failures — `0/5 → 0/5` из-за отсутствия tool calls. Measurement path подтверждён; влияние compact rewrite и преимущество repair при уже валидном direct не заявляются. Время provider не используется для вывода; production-shaped evidence не закрывает production evaluation.

### Legacy flow compatibility

Exact `takt-flow-evaluation/v1alpha1` `eval flow` executes isolated flow suites through the application control path,
persists repeat evidence and supports `eval flow init` for a validator-free
skeleton. Repeat evidence preserves a secret-checked full-history
`repository.bundle`, the redacted final product source tree and baseline-to-final
Git diff before workspace cleanup; `.git/` and `.takt/` are excluded from the
source copy. `--trace` streams elapsed suite stages, durable root progress and
terminal child Run/node statuses plus live bounded Pi tool/message progress to
stderr using aligned `SCOPE | EVENT | DETAILS` lines. Run/node context is placed
at the start, full IDs are announced once, and repeated tool events omit stable
model/session metadata. Pi RPC excludes cumulative partial/UI noise from durable stdout while
retaining strict per-record limits; transient streaming updates reset inactivity
without becoming durable events. The 30-second heartbeat reports effective
idle/limit/last activity for root and child assistants and the last measured
model-request input tokens when Pi exposes optional message usage; unavailable
context is explicit. Flow evaluation applies a configurable `5m`
assistant idle fallback when a production node has no explicit `idle_timeout`;
provider stalls therefore end durably as `timed_out`. Deterministic contracts cover report
persistence and worktree ordering; live Pi evidence has confirmed fresh/exact
resume and a completed production `implement` node, while a complete multi-node
quality result remains separate evidence.
Каждый запуск оценки процесса атомарно публикует `progress.json`; команды
`eval status` и `make eval-status RUN=...` без запуска модели показывают
текущий сценарий и фазу,
сохраняемый прогресс Run и узлов, активную фазу провайдера Pi, сведения о
повторах и измеренную сохранённую статистику. В изолированных каталогах Pi
контур оценки отключает внутренние повторы Pi, поэтому единственным владельцем
сохраняемого цикла повторов остаётся Takt. Остальные настройки изолированного
проекта Pi задаются
нативным объектом `assistants.<name>.settings`; корпус mini-du фиксирует
`httpIdleTimeoutMs` на пяти минутах. Завершённый или неуспешный снимок остаётся
рядом с отчётом; поле `updated_at` позволяет обнаружить устаревший снимок со
статусом `running` после завершения процесса.
`eval stats` provides a compact human/JSON view of one saved suite report and
falls back to a `status=running`, `complete=false` partial snapshot from
`progress.json` before the first report checkpoint. The partial view contains
live counters and timings but no unavailable per-case/per-execution details.
Completed reports separate total node attempts from actual assistant executions
and list each assistant step with model, tokens and durable-event wall time. Old reports
without node timing remain readable and show unavailable duration. A separate
assistant-session table exposes the full durable Session ID for each execution,
including attempt/provider-attempt and fresh/resume mode.
When a checkpointed report is followed by a terminal progress failure, stats keeps
the report details, preserves `status=failed`, and sets `complete` from whether
all planned runs reached final results.
Команда `eval stats` также определяет одну основную причину отказа для каждого
сценария. Команды `eval inspect` и
`make eval-inspect RUN=... [CASE=...] [REPEAT=...]` выполняют
детерминированное расследование только для чтения по сохранённым состояниям
валидатора, среды выполнения и узлов, diff, исходникам, Git, артефактам и
свидетельствам SCM, а также
по отфильтрованным и отредактированным событиям запуска инструментов и
жизненного цикла Pi, включая наблюдаемое клиентом время ожидания, потока и
полную длительность. `CAUSAL CHAIN` связывает доказанный лимит вывода ассистента,
фактическое число вызовов инструментов, пустой завершённый результат,
детерминированный отказ валидации и пропущенные зависимые узлы, а не повторяет
один вердикт валидатора. Команды не обращаются к модели и не изменяют вердикт
валидатора; недоступные свидетельства остаются явными, а эвристические
наблюдения не выдаются за установленные факты.
`eval compare` renders an A/B scorecard with an explicit overall,
correctness/reliability/efficiency and per-metric `BETTER|WORSE|SAME` direction,
human resource deltas, presets/models and per-case transitions. Missing
measurements are distinguished from non-comparable values. Make wrappers accept
`RUN` and short `A`/`B` paths and never launch model runs implicitly.
The mini-du validator v3 adds `hardlink_multiple` and `double_dash_default` to
the oracle and requires the explicit per-case CLI surface `-s`, `-k`,
`-H`, `-h`/`--help`, `--`, `-sk`/`-ks`/`-sH`, and fail-closed unknown options;
the smoke target runs one case and the full feature target runs all three.
Validator-3 runs are measurement generation v2, validator-4 runs are v3;
registry-old runs are v0 and the corrected-registry/validator-2 baseline is v1. Cross-generation trend
comparisons are forbidden. `code:feature-development` gates its implementation retry on a non-empty regular
`implementation.md` before starting review. The validator reports a missing
record as `missing_artifact`, while option and missing-path diagnostics are
checked by exit/stdout/stderr behavior without requiring host-specific text.
Invalid flow results do not publish a per-run time-to-valid value.

`eval analyze` executes the fixed read-only `evaluation:analyze` workflow through
the same application/runFlowCase boundary. It selects non-`true_accept` cases by
default, requires `takt_analyze`, persists redacted timestamped manifests and
structured advisory reports, including redacted prompt fingerprints, deterministic
inspection context, citation-checked evidence, analyzer session evidence and
trace, and leaves the source evaluation report unchanged. The generated
`evidence-manifest.json` is a validated citation target; relative analyzer
session paths are resolved inside the execution workspace, and bounded redacted
raw model output is retained for protocol failures. Citations that repeat the
manifest's `evidence_root/` prefix are normalized only to a listed evidence
file. Equivalent `#/pointer`, `path:line-range`, and zero-based text `/N`
forms are normalized to the canonical citation syntax. Completed analyses must
state a causal mechanism, classify the failure point and name a concrete
prevention, backed by at least one checked non-validator runtime/assistant/
artifact/source/diff/SCM citation; the deterministic verdict remains immutable.
The analyzer accepts `--language en|ru` (default `en`); the selected language
is persisted in the analysis report and manifest while schema keys remain
stable. `failure_mode` remains an untranslated lowercase snake_case machine
code so reports in different languages stay comparable.

The evaluation fake `gh` derives its immutable fixture and mutable SCM log
paths from runtime-provided `TAKT_WORKSPACE`, falling back to its installed
`.takt/eval/bin/gh` location. Assistant-provided `FAKE_GH_*` overrides cannot
redirect eval side effects. Standalone fixture tests retain the environment-
based mode when neither trusted workspace source is available.

Flow evaluation polling tolerates the transient absence of `state.json` after
detached `run.start` has returned an accepted Run ID. It waits for the first
durable state while preserving fail-fast handling for launch and Store errors.

`eval-status` renders elapsed duration, completed case/node percentages, input,
output and total durable tokens, phase timings, observed assistant wait/stream/
total/tool timings, the current measured model context when an assistant has
emitted it, and a separate valid-rate percentage over already validated cases;
zero denominators are shown as `n/a`.

## Предметные поставки

- профиль `code` 0.19.4: 19 workflow, deterministic `plan-to-pr` acceptance, outcome-gated `feature-development` и trusted block catalog;
- Route DSL examples/eval corpus;
- authoring skill;
- multi-repo/reference fake adapters.


## v0.2 contract convergence — v0.1.48

- `input.schema` и `output_format` используют единый `takt-schema-subset/v1`; полный JSON Schema не заявляется;
- `takt compatibility matrix|fields|schema|check` публикует support/verification boundaries и проверяет конкретный Config;
- field matrix покрывает public JSON fields stable-candidate Workflow/Node/Config/BlockPackage и явно defer-ит alpha seams;
- Pi/OpenCode session adapter compatibility отделена от host-control enforcement; bundled host integrations остаются guarded до live conformance;
- process `takt-assistant/v1alpha1` помечен deprecated для новых wrappers, `v1alpha2` остаётся целевым public protocol.

## P2 External seams — v0.1.49–v0.1.50

- `cmd/qwen-takt-adapter` использует только `sdk/agentadapter`, преобразует официальный Qwen Code headless stream-json в v1alpha2 и поддерживает exact resume;
- process v1alpha2 больше не означает автоматический `tool_control`: configured capabilities должны подтверждаться stream declaration;
- `cmd/takt-github-scm-adapter` использует только `sdk/domainadapter`, реализует neutral SCM operations и reconcile через hashed marker;
- domain Invoke/Reconcile request содержит execution `workspace`, process transport использует его как cwd;
- multi-repo `publish_change` передаёт точный `repository_workspace` candidate worktree;
- `tests/e2e` / `TestReferenceAdaptersBoundary` доказывает оба seams без сетевых credentials;
- `v0.1.50` добавляет `takt-task-source/v1alpha1`, public `sdk/tasksource`, `source + source_ref` в Task API и reference GitHub Issue source до Router.

## P3 Human-reviewed learning — v0.1.51

- `takt learn scan` находит repeated diagnostic/workflow fingerprints минимум в двух distinct Run;
- `learn propose` создаёт durable `takt-learning/v1alpha1` proposal с supporting Run IDs, expected benefit и immutable candidate SHA-256;
- skill и BlockPackage candidates проходят локальную structural validation и snapshot без symlink;
- `learn review` требует явного решения человека и rationale;
- `learn evaluate` принимает только versioned matrix reports с `matrix_fingerprint`, `benchmark_id` и regression gates;
- `learn stage` повторно проверяет candidate hash и пишет только `.takt/learning/ready`, не изменяя trusted packages/skill config;
- `tests/e2e: TestLearningLoopContract` закрепляет весь gate end-to-end.

## Архитектурная стабилизация — v0.1.52

- `cmd/takt` оставлен только launcher; transport parsing перенесён в разбитый по областям `internal/cli`;
- монолитный `internal/control.Service` удалён и заменён application services по use cases;
- `internal/bootstrap` является production composition root;
- application использует consumer-owned `RunStore`, а production связывает его с `store.FS`;
- daemon и общие MCP operations используют canonical `internal/appapi` registry;
- MCP production constructor получает только API/Plan/External/Maintenance dependencies;
- daemon/MCP background lifecycle унифицирован через `MaintenanceService`;
- runtime construction использует explicit `Definition + Dependencies`;
- node action execution и workflow validation разложены по ответственности без plugin/DI framework;
- `go test ./internal/architecture` закрепляет import boundaries и thin `cmd/takt` в release gate.

Внешние workflow/config/state/protocol contracts в этом срезе не менялись.

## Тестовая архитектура — v0.1.53

- product correctness проверяется стандартными Go `*_test.go`;
- black-box CLI/daemon/MCP/evaluation contracts находятся в `tests/e2e` и используют общий Go harness;
- schema registry contract перенесён из Python в `internal/schemacontract`;
- 38 прежних `scripts/test-*.sh` сокращены до пяти внешних smoke boundaries;
- `internal/architecture` содержит allowlist shell smoke tests и блокирует возврат второго shell test framework;
- historical Makefile contract targets сохранены, но Go-доступная семантика запускается напрямую через `go test`.

Внешние product contracts не менялись. Детали — `docs/67-go-native-test-architecture-v0.1.53.md`, ADR-086.

## Architecture hardening — v0.1.54

- shared `application.Context` удалён; каждый service хранит private dependencies своего use case;
- Run↔Plan dependency cycle разорван отдельным `ForkService`; service graph проверяется AST-gate;
- concrete stores/backends/factories собираются bootstrap-слоем; evaluation/runtime не имеют второго production composition root;
- CLI создаёт signal-aware root context; foreground lifecycle проходит до runtime, durable detach обозначен явно;
- Dynamic Plan/Host Control используют durable store locks вместо process-global mutex;
- Task response, Dynamic Plan advance и child fan-out разложены по фазам без plugin framework;
- appapi/MCP используют единую canonical operation identity;
- shell `test-*.sh` сокращён до одного TypeScript compiler smoke, остальные внешние boundaries проверяются bounded Go E2E.

Внешние product contracts не менялись. Детали — `docs/68-architecture-hardening-v0.1.54.md`, ADR-087.

## Фактические незакрытые gaps

1. Live Route DSL production evidence.
2. Обезличенная Go + Document production evaluation; первый Go production-shaped live-срез выполнен, но не заменяет production corpus. Настройка workflow и `eval-feature` на внешнем реальном проекте выполняется пользователем отдельно и не входит в repo-owned изменения до передачи evidence.
3. Финальная v0.2/v1beta1 migration после production evidence; schema subset, field audit и compatibility matrix закрыты в v0.1.48.
4. Live strict host conformance Pi/OpenCode: fresh/resume и часть guarded host capabilities подтверждены на Pi `0.83.0`/OpenCode `1.18.14`, но tool/completion и часть Pi boundaries остаются непроверенными; новая pinned host version требует нового полного evidence.
5. Live Qwen/GitHub smoke reference adapters с внешними credentials при внедрении; public SDK/reference implementations закрыты в v0.1.49–v0.1.50.
6. Workflow graph/explain/scaffold и статический reject/revise contract.
7. Archon-first Takt YAML из `docs/superpowers/specs/2026-08-11-archon-compatible-flow-runtime-spec.md`: A0 language switch и A1 loop/session/recovery semantics реализованы и покрыты Go contract/E2E tests. Единый native language surface использует target root/node/provider и `$...` references; legacy Workflow root, frontmatter `assistant` и `${...}` отклоняются. Реализованы `loop`, scalar/structured `until`, `until.signal`, `until.requires`, `until_bash`, durable signal/predicate evidence, `fresh_context`, `context: shared`, exact Session ID resume, cancel metadata и retry history. Сохранён `output_format`/schema-subset contract. Отдельный importer/transpiler и второй executor не планируются. Deferred остаются hard token/tool budgets до live capability proof, дополнительные iteration/evidence projections `run inspect` и mutating merge fan-out.

Подробный порядок — `06-roadmap.md`; задачи — `14-backlog-v0.2.md`.


## Structured Task Sources v0.1.50

Реализован ingress-контракт `takt-task-source/v1alpha1`: `task start` и `takt.task.start` принимают `source + source_ref`, source adapter формирует normalized Task с immutable revision, а Router/Planner/Replanner получают структурированный `task_source`. Reference GitHub Issue adapter использует public `sdk/tasksource`; provider-specific ingestion не входит в runtime.
