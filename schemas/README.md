# Машиночитаемые схемы

- `config.schema.json` — текущий `takt/v1alpha1 Config`, включая `mock`, `process`, `pi`, Pi-specific `binary/args/session_dir/project_trust`, `max_output_bytes` и условные запреты несовместимых полей;
- `workflow.schema.json` — текущий `takt/v1alpha1 Workflow`, включая `timeout`;
- `command-frontmatter.schema.json` — frontmatter Markdown-команд;
- `run-state.schema.json` — состояние Run, fingerprints, revisions, типизированные Node statuses, execution identity и aggregate usage;
- `event.schema.json` — JSONL-событие с revision;
- `assistant-protocol.schema.json` — реализованный JSON-протокол `takt-assistant/v1alpha1` со строгими status/exit и неотрицательным usage;
- `validation-result.schema.json` — предметно-независимый результат качества `takt-validation/v1alpha1` для benchmark и внешних валидаторов;
- `evaluation-report.schema.json` — отчёт `takt-evaluation/v1alpha1` с идентичностью стратегии, benchmark, workspace, моделей и метриками качества.

Go-loader остаётся главным валидатором текущей реализации: кроме структуры он проверяет DAG, ссылки на модели/исполнителей, duration и ограничения `loop_group`. JSON Schema предназначены для редакторов, внешних инструментов и подготовки стабильной схемы.

`run-state.schema.json` включает подтверждённый флаг `nodes.*.resumed`, assistant/version, requested/resolved model и aggregate usage всех выполненных агентных попыток.
