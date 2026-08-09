# Текущее состояние реализации

Статус после `v0.1.57-alpha`. Документ описывает фактическое состояние, а не исторический backlog.

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

Bundled Pi/OpenCode host integrations остаются `guarded`, пока command/input/tool/completion/recovery contract не подтверждён live smoke на конкретной версии host.

## Dynamic Takt — реализовано

- Task Router `workflow|template|dynamic`;
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

## Предметные поставки

- профиль `code` 0.16.0: 19 workflow и trusted block catalog;
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
2. Go + Document production evaluation.
3. Финальная v0.2/v1beta1 migration после production evidence; schema subset, field audit и compatibility matrix закрыты в v0.1.48.
4. Live strict host conformance Pi/OpenCode.
5. Live Qwen/GitHub smoke reference adapters с внешними credentials при внедрении; public SDK/reference implementations закрыты в v0.1.49–v0.1.50.
6. Workflow graph/explain/scaffold и статический reject/revise contract.

Подробный порядок — `06-roadmap.md`; задачи — `14-backlog-v0.2.md`.


## Structured Task Sources v0.1.50

Реализован ingress-контракт `takt-task-source/v1alpha1`: `task start` и `takt.task.start` принимают `source + source_ref`, source adapter формирует normalized Task с immutable revision, а Router/Planner/Replanner получают структурированный `task_source`. Reference GitHub Issue adapter использует public `sdk/tasksource`; provider-specific ingestion не входит в runtime.
