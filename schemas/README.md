# Машиночитаемые схемы

- `cli-envelope.schema.json` — стабильный верхнеуровневый JSON-конверт CLI: `{"ok":true,"result":...}` или структурированная ошибка;
- `learning-proposal.schema.json` — durable proposal human-reviewed Skill/Block Learning Loop: повторяемый pattern, immutable candidate snapshot, решение ревью, evaluation evidence и staged ready-path;
- `schema-subset-v1.schema.json` — meta-schema для `takt-schema-subset/v1`, общего контракта `input.schema` и `output_format`;
- `schema-subset-description.schema.json` — payload `takt compatibility schema`, публикующий точную версию, keywords и неподдерживаемые конструкции subset;
- `compatibility-matrix.schema.json` — machine-readable support/verification matrix session adapters, host integrations, domain adapters и MCP surfaces;
- `compatibility-check.schema.json` — отчёт `takt compatibility check` для конкретного Config;
- `v1beta1-field-matrix.schema.json` — field-level решения `keep|migrate-value|defer|...` для будущей границы v1beta1;
- `block-package.schema.json` — manifest доверенного BlockPackage, governance, requirements и зависимости;
- `domain-adapter-protocol.schema.json` — process-протокол публичного `sdk/domainadapter`;
- `package-lock.schema.json` — воспроизводимый lock установленных packages;
- `package-policy.schema.json` — source/signature policy для package distribution;
- `package-signature.schema.json` — detached Ed25519 signature metadata package;
- `task-brief.schema.json` — скомпилированный bounded brief роли/фазы Dynamic Takt;
- `task-source.schema.json` — нормализованный provider-neutral Task с immutable source provenance до Router;
- `task-source-protocol.schema.json` — process-протокол `takt-task-source/v1alpha1` для внешних Task Source adapters;
- `workflow-plan.schema.json` — ограниченный `WorkflowPlan` Dynamic Takt;
- `config.schema.json` — текущий `takt/v1alpha1 Config`, включая `default_assistant`, `mock`, `process`, `pi`, `opencode`, специфичные для Pi `session_dir/project_trust/settings` и специфичные для OpenCode `agent/auto_approve`, `max_output_bytes` и условные запреты несовместимых полей;
- `task-route.schema.json` — проверяемое решение Task Router: `workflow|template|dynamic`, сигналы и прогрессивные controls;
- `evidence-manifest.schema.json` — внутренний EvidenceManifest: baseline, fingerprints известных failures, check-to-evidence mapping и verdict, привязанный к candidate SHA-256;
- `workspace.schema.json` — bounded multi-repo `takt/v1alpha1 Workspace`: repository IDs, relative paths and acyclic `depends_on`;
- `workflow.schema.json` — текущий `takt/v1alpha1 Workflow` с `loop_group.max_iterations <= 64`, включая `timeout`, `idle_timeout`, `attempts.backoff`, `sandbox.enforcement`, `always_run`, расширенный `output_format`, `one_success`, approval в цикле, `foreach.parallel` и governed child `workflow`;
- `profile.schema.json` — Profile с default workflow и картой именованных `workflows`;
- `command-frontmatter.schema.json` — frontmatter Markdown-команд;
- `run-state.schema.json` — состояние Run, parent/child links, pause/abandon/recovery/operator retry, canonical `NodePath`, bounded `loop_iterations[]`, diagnostics/retry/sandbox decisions, fingerprints, revisions, типизированные Node statuses, execution identity и aggregate usage;
- `event.schema.json` — JSONL-событие с revision;
- `notification-config.schema.json` — локальные attention/terminal события и sinks `coding_agent_host|desktop|process`;
- `assistant-protocol.schema.json` — реализованный JSON-протокол `takt-assistant/v1alpha1|v1alpha2` со строгими status/exit и неотрицательным usage;
- `validation-result.schema.json` — предметно-независимый результат качества `takt-validation/v1alpha1` для benchmark и внешних валидаторов;
- `assessment.schema.json` — immutable `takt-assessment/v1alpha1`, связывающий validation result и evidence с terminal result revision оцениваемого Run;
- `evaluation-report.schema.json` — отчёт `takt-evaluation/v1alpha1` с идентичностью стратегии, benchmark, workspace, моделей, optional wall-clock duration flow-узлов и метриками качества.
- `evaluation-analysis.schema.json` — advisory `takt-evaluation-analysis/v1alpha1` report with deterministic evidence, read-only model analysis, session metadata and structured diagnosis.
- `evaluation-case-manifest.schema.json` — labels `category|difficulty|source` и другие стабильные метаданные cases;
- `evaluation-matrix.schema.json` — сравнительная matrix стратегий, baseline, repeat и regression gates;
- `evaluation-compare.schema.json` — попарное A/B сравнение по case ID + repeat с output directories, quality/resource metrics и transitions;
- `evaluation-stats.schema.json` — компактная human/JSON статистика одного сохранённого suite run с node attempts, assistant executions, assistant-step timing и Session IDs;
- `evaluation-inspection.schema.json` — детерминированное read-only расследование причин и сохранённых evidence одного flow evaluation, включая optional executor manifest;
- `evaluation-matrix-report.schema.json` — итог `takt-evaluation-matrix/v1alpha1` со строго типизированными strategy summaries, usage breakdown, comparisons и gate results.
- `flow-evaluation-suite.schema.json` — strict production flow evaluation suite.
- `flow-evaluation-progress.schema.json` — атомарный внешний snapshot текущей фазы, Run/node progress и измеренных live-агрегатов production flow evaluation.
- `evaluation-validator-request.schema.json` — validator invocation protocol.
- `executor-manifest.json` — per-repeat `takt-evaluation-executor/v1alpha1` adapter/session evidence manifest с bounded redacted session copies.

Flow suites are created with `takt eval flow init`; the scaffold intentionally
does not generate a validator or any executable code.
- `task-case-manifest.schema.json` — task-level cases с ожидаемым route/status и минимальной ревизией плана;
- `task-evaluation-matrix.schema.json` — matrix для полного `Task Router → Dynamic Plan → replan` контура;
- `task-evaluation-report.schema.json` — task-level report с route accuracy, plan revisions, replanner runs, pairwise outcomes и gates.

`evaluation-analysis.schema.json` is advisory only: its deterministic fields are
copied from the saved evaluation, while model diagnosis and citations never
replace validation or benchmark quality metrics. Reports retain the redacted
rendered prompt/fingerprint and trace/session evidence metadata; citations are
accepted only when they resolve inside the bounded evidence manifest.

Go-loader и authoring preflight остаются главным валидатором: кроме структуры они проверяют DAG, ссылки на модели/исполнителей, capabilities, duration, template/output/artifact references и ограничения `loop_group`. JSON Schema предназначены для редакторов, внешних инструментов и подготовки стабильной схемы.

`run-state.schema.json` включает подтверждённый флаг `nodes.*.resumed`, aggregate-поля узла и массив `nodes.*.executions` с assistant/version, requested/resolved model и usage каждой фактической попытки.

`evaluation-report.schema.json` всегда сериализует измеряемые нулевые показатели. Недоступные средние значения представлены как `null`. Usage распределяется по `usage_by_execution_identity`, а узлы с разными моделями или версиями между попытками помечаются `mixed_execution_identity`. Поле `amortized_end_to_end_ms_per_valid` отражает суммарную длительность всех Run на один корректный результат и не является временем достижения валидности внутри отдельного Run.

`run-state.schema.json` and `evaluation-report.schema.json` expose separate `stdout` and `stderr` fields for node results. The compatibility field `output`/`diagnostic_output` remains the combined diagnostic representation. Structured validation results are decoded only from `stdout`.

`workflow.schema.json` также описывает reusable `subworkflow`, последовательный/параллельный `foreach` с `items` или `items_from.path`, проверяемый JSON output и композицию с approval внутри `loop_group`. Подключённые workflow проверяются той же схемой после загрузки и компиляции.

`workflow.schema.json` различает structural `subworkflow` и governed `workflow`. Последний задаёт `path`, `input`, `output_node` и `isolation`. `run-state.schema.json` описывает `parent_run_id`, `parent_node_id`, `child_run_ids`, waiting kind/link, Run output/usage/cancel state и историю child attempts на узле.

Начиная с `v0.1.44`, `attempts.backoff` имеет durable deadline в RunState, а `sandbox.enforcement` описывает только локальное OS enforcement для deterministic `bash/script` узлов. Assistant-level `sandbox.filesystem/network` остаётся capability-контрактом adapter. `run-state.schema.json` фиксирует diagnostic fingerprint, retry deadline и фактическое sandbox decision для воспроизводимого resume.

## v0.1.49 external adapter seam

`domain-adapter-protocol.schema.json` includes optional `workspace` in Invoke/Reconcile requests. It is the execution workspace/cwd supplied by Takt; provider-specific repository identity stays in the operation input/config. Reference implementations are documented in `docs/63-reference-external-adapters-v0.1.49.md`.


## v0.1.50 structured task sources

`task-source.schema.json` описывает normalized Task/provenance, `task-source-protocol.schema.json` — автономный process protocol `takt-task-source/v1alpha1`, а `schema-subset-description.schema.json` — machine-readable payload `takt compatibility schema`. Все схемы реестра проверяются Go-контрактом `internal/schemacontract`: только local `$ref`, Draft 2020-12 marker и регистрация каждой schema в этом файле.
