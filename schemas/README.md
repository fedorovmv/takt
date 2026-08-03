# Машиночитаемые схемы

- `config.schema.json` — текущий `takt/v1alpha1 Config`, включая `max_output_bytes`;
- `workflow.schema.json` — текущий `takt/v1alpha1 Workflow`, включая `timeout`;
- `command-frontmatter.schema.json` — frontmatter Markdown-команд;
- `run-state.schema.json` — состояние Run, fingerprints, revisions и типизированные Node statuses;
- `event.schema.json` — JSONL-событие с revision;
- `assistant-protocol.schema.json` — реализованный JSON-протокол `takt-assistant/v1alpha1`.

Go-loader остаётся главным валидатором текущей реализации: кроме структуры он проверяет DAG, ссылки на модели/исполнителей, duration и ограничения `loop_group`. JSON Schema предназначены для редакторов, внешних инструментов и подготовки стабильной схемы.
