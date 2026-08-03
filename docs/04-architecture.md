# Архитектура

```text
CLI
 │
 ▼
Workflow Loader ── Command Resolver ── Config/Model Registry
 │
 ▼
Runner / DAG Scheduler
 ├── Agent Node ── Assistant Adapter ── Pi/OpenCode/другой CLI
 ├── Bash Node
 ├── Approval Node
 ├── Loop Group
 └── Hook Runtime
 │
 ├── State Store (state.json)
 ├── Event Log (events.jsonl)
 └── Artifact Store (filesystem)
```

## Пакеты

- `internal/yamlmini` — минимальный YAML 1.2 subset без внешних зависимостей;
- `internal/spec` — публичные структуры схемы;
- `internal/config` — модели и исполнители;
- `internal/command` — Markdown-команды;
- `internal/assistant` — адаптеры `mock` и `process`;
- `internal/workflow` — загрузка и статическая проверка DAG;
- `internal/runtime` — выполнение, hooks, pause/resume, события;
- `internal/store` — состояние и журнал.

## Граница с кодовым агентом

Takt не получает отдельные вызовы `read_file`, `write_file` и MCP tools. Он видит агентный запуск как одну операцию. Детальные события доступны только при наличии специального адаптера и capability, что можно добавить позже без изменения workflow runtime.

## Протокол process assistant

Базовый адаптер запускает настроенный процесс и передаёт параметры через argv, stdin и переменные окружения:

```text
HARNESS_MODEL_ID
HARNESS_MODEL_PROVIDER
HARNESS_SESSION_MODE
HARNESS_SESSION_ID
HARNESS_WORKSPACE
HARNESS_NATIVE_HOOKS_JSON
```

Stdout становится output узла. Ненулевой код завершения считается ошибкой запуска агента.
