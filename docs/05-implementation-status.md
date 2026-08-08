# Текущее состояние реализации

Статус после `v0.1.52-alpha`. Документ описывает фактическое состояние, а не исторический backlog.

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
- `scripts/test-reference-adapters.sh` доказывает оба seams без сетевых credentials;
- `v0.1.50` добавляет `takt-task-source/v1alpha1`, public `sdk/tasksource`, `source + source_ref` в Task API и reference GitHub Issue source до Router.

## P3 Human-reviewed learning — v0.1.51

- `takt learn scan` находит repeated diagnostic/workflow fingerprints минимум в двух distinct Run;
- `learn propose` создаёт durable `takt-learning/v1alpha1` proposal с supporting Run IDs, expected benefit и immutable candidate SHA-256;
- skill и BlockPackage candidates проходят локальную structural validation и snapshot без symlink;
- `learn review` требует явного решения человека и rationale;
- `learn evaluate` принимает только versioned matrix reports с `matrix_fingerprint`, `benchmark_id` и regression gates;
- `learn stage` повторно проверяет candidate hash и пишет только `.takt/learning/ready`, не изменяя trusted packages/skill config;
- `scripts/test-learning-loop.sh` закрепляет весь gate end-to-end.

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
- `scripts/test-architecture.sh` закрепляет import boundaries и thin `cmd/takt` в release gate.

Внешние workflow/config/state/protocol contracts в этом срезе не менялись.

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
