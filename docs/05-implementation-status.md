# Текущее состояние реализации

Статус после `v0.1.46-alpha`. Документ описывает фактическое состояние, а не исторический backlog.

## Ядро runtime — реализовано

- один DAG scheduler для root/composition/dynamic compiled workflows;
- dependencies, `when`, trigger rules, `always_run`, retries/backoff, hooks, timeout/cancel;
- sequential/parallel `foreach`, `loop_group`, `subworkflow`, governed child `workflow`;
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

Production provider adapters намеренно не встроены в ядро. Fake adapters доказывают контракт; GitHub/корпоративные реализации остаются внешними поставками.

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

## Фактические незакрытые gaps

1. Live Route DSL production evidence.
2. Go + Document production evaluation.
3. v0.2/v1beta1 contract stabilization и migration.
4. Full iteration history / решение по nested `loop_group`.
5. Live strict host conformance Pi/OpenCode.
6. Один реальный external coding-agent wrapper и один production-like Domain Adapter.
7. Structured task source adapter.
8. Human-reviewed skill/block learning loop.
9. Workflow graph/explain/scaffold и статический reject/revise contract.

Подробный порядок — `06-roadmap.md`; задачи — `14-backlog-v0.2.md`.
