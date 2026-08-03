# Машиночитаемые схемы

- `config.schema.json` — текущий `takt/v1alpha1 Config`;
- `workflow.schema.json` — текущий `takt/v1alpha1 Workflow`;
- `command-frontmatter.schema.json` — frontmatter Markdown-команд;
- `run-state.schema.json` — текущее локальное состояние Run;
- `event.schema.json` — текущий формат JSONL-события;
- `assistant-protocol.schema.json` — целевой контракт адаптера v0.2, ещё не полностью реализованный.

Go-loader остаётся главным валидатором текущей реализации: кроме структуры он проверяет DAG, ссылки на модели/исполнителей и ограничения `loop_group`. JSON Schema предназначены для редакторов, внешних инструментов и подготовки будущей стабильной схемы.
