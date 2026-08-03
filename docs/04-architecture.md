# Архитектура

```text
CLI
 │
 ▼
Workflow Loader ── Command Resolver ── Config/Model Registry
 │
 ▼
Runner / Shared DAG Scheduler
 ├── Agent Node ── Assistant Adapter ── Pi/OpenCode/другой CLI
 ├── Bash Node
 ├── Approval Node
 ├── Loop Group ── тот же DAG Scheduler
 └── Hook Runtime
 │
 ├── Definition Fingerprints
 ├── Revisioned State/Event Store
 └── Artifact Store
```

## Пакеты

- `internal/yamlmini` — документированный YAML subset без внешних зависимостей;
- `internal/spec` — структуры текущей схемы;
- `internal/config` — модели и исполнители;
- `internal/command` — Markdown-команды;
- `internal/execution` — классы ошибок и управление process group;
- `internal/assistant` — адаптеры `mock`, универсальный `process` и специализированный `pi`;
- `internal/definition` — fingerprints workflow/config/commands;
- `internal/workflow` — загрузка и статическая проверка DAG;
- `internal/runtime` — общий scheduler, hooks, loops, approval и итог Run;
- `internal/store` — revisioned state/event store и lock Run.

## Граница с кодовым агентом

Takt не получает отдельные вызовы `read_file`, `write_file` и MCP tools. Агентный запуск является одной операцией. Детальные события появятся только в specialized adapter при наличии соответствующей capability.

## Семантика выполнения

Failure узла не завершает Run немедленно. Node становится terminal, после чего scheduler выполняет разрешённые ветви, включая `all_done`. Корневой workflow и дочерний DAG `loop_group` используют один механизм. Завершение attempt context имеет приоритет над производными ошибками контейнера, поэтому timeout/cancellation дочерней работы сохраняются на родительском `loop_group`.

## Process assistant

Базовый адаптер запускает настроенный процесс и передаёт параметры через argv, stdin и окружение:

```text
TAKT_MODEL_NAME
TAKT_MODEL_ID
TAKT_MODEL_PROVIDER
TAKT_MODEL_PARAMS_JSON
TAKT_SESSION_MODE
TAKT_SESSION_ID
TAKT_WORKSPACE
TAKT_NATIVE_HOOKS_JSON
```

Ненулевой exit code классифицируется как `exit`; ошибка запуска — `start`; context timeout — `timed_out`; cancellation — `cancelled`.


## Pi assistant

Специализированный `pi` adapter запускает `pi --mode rpc`, передаёт prompt по JSONL и получает Session ID, итоговый текст, статистику и фактическую модель через RPC-команды. `fresh` создаёт новую сессию, `resume` передаёт сохранённый ID через `--session` и проверяет его по `get_state`. Инструменты, файлы, shell, skills и история остаются внутри Pi.

## Состояние

Каждый transition записывается через `Store.Commit`. State и event получают одну revision. `Load` обнаруживает рассогласование. `answer` и `resume` получают lock и проверяют fingerprints определений.

## Scope безопасности

Текущая архитектура предполагает trusted local input. Наличие path validation для Run ID и process limits не делает runtime безопасным для недоверенных workflow.
