# Машиночитаемые схемы

- `config.schema.json` — текущий `takt/v1alpha1 Config`, включая `mock`, `process`, `pi`, `opencode`, Pi-specific `session_dir/project_trust` и OpenCode-specific `agent/auto_approve`, `max_output_bytes` и условные запреты несовместимых полей;
- `workflow.schema.json` — текущий `takt/v1alpha1 Workflow`, включая `timeout`, `idle_timeout`, `always_run`, расширенный `output_format`, `one_success`, approval в цикле, `foreach.parallel` и governed child `workflow`;
- `profile.schema.json` — Profile с default workflow и картой именованных `workflows`;
- `command-frontmatter.schema.json` — frontmatter Markdown-команд;
- `run-state.schema.json` — состояние Run, parent/child links, cancellation, fingerprints, revisions, типизированные Node statuses, execution identity и aggregate usage;
- `event.schema.json` — JSONL-событие с revision;
- `assistant-protocol.schema.json` — реализованный JSON-протокол `takt-assistant/v1alpha1` со строгими status/exit и неотрицательным usage;
- `validation-result.schema.json` — предметно-независимый результат качества `takt-validation/v1alpha1` для benchmark и внешних валидаторов;
- `evaluation-report.schema.json` — отчёт `takt-evaluation/v1alpha1` с идентичностью стратегии, benchmark, workspace, моделей и метриками качества.

Go-loader и authoring preflight остаются главным валидатором: кроме структуры они проверяют DAG, ссылки на модели/исполнителей, capabilities, duration, template/output/artifact references и ограничения `loop_group`. JSON Schema предназначены для редакторов, внешних инструментов и подготовки стабильной схемы.

`run-state.schema.json` включает подтверждённый флаг `nodes.*.resumed`, aggregate-поля узла и массив `nodes.*.executions` с assistant/version, requested/resolved model и usage каждой фактической попытки.

`evaluation-report.schema.json` всегда сериализует измеряемые нулевые показатели. Недоступные средние значения представлены как `null`. Usage распределяется по `usage_by_execution_identity`, а узлы с разными моделями или версиями между попытками помечаются `mixed_execution_identity`. Поле `amortized_end_to_end_ms_per_valid` отражает суммарную длительность всех Run на один корректный результат и не является временем достижения валидности внутри отдельного Run.

`run-state.schema.json` and `evaluation-report.schema.json` expose separate `stdout` and `stderr` fields for node results. The compatibility field `output`/`diagnostic_output` remains the combined diagnostic representation. Structured validation results are decoded only from `stdout`.

`workflow.schema.json` также описывает reusable `subworkflow`, последовательный/параллельный `foreach` с `items` или `items_from.path`, проверяемый JSON output и композицию с approval внутри `loop_group`. Подключённые workflow проверяются той же схемой после загрузки и компиляции.

`workflow.schema.json` различает structural `subworkflow` и governed `workflow`. Последний задаёт `path`, `input`, `output_node` и `isolation`. `run-state.schema.json` описывает `parent_run_id`, `parent_node_id`, `child_run_ids`, waiting kind/link, Run output/usage/cancel state и историю child attempts на узле.
