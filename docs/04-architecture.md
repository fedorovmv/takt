# Архитектура

```text
CLI
 │
 ▼
Control Workspace / Run Store ── Git Worktree Manager ── Execution Workspace
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
 ├── Structural Subworkflow/Foreach ── скомпилированный DAG
 ├── Governed Workflow Node ── Child Runner / отдельный Run Store entry
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
- `internal/gitworktree` — создание ветки/worktree, проверка dirty state, удаление и prune;
- `internal/assistant` — адаптеры `mock`, универсальный `process` и специализированные `pi` и `opencode`;
- `internal/definition` — fingerprints workflow/config/commands;
- `internal/workflow` — загрузка и статическая проверка DAG;
- `internal/runtime` — общий scheduler, hooks, loops, approval, governed child lifecycle, cancellation tree и итог Run;
- `internal/store` — revisioned state/event store, parent/child links, durable cancel markers, aggregate usage и lock Run;
- `internal/evaluation` — изолированный запуск наборов заданий и агрегация метрик из RunState.

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

Специализированный `pi` adapter запускает `pi --mode rpc`, передаёт prompt по JSONL и получает Session ID, итоговый текст, статистику и сообщения через RPC-команды. Версия Pi сохраняется из version probe, а фактическая модель определяется по `responseModel` последнего assistant message с fallback на выбранную модель сессии. `fresh` создаёт новую сессию, `resume` передаёт сохранённый ID через `--session` и проверяет его по `get_state`. Инструменты, файлы, shell, skills и история остаются внутри Pi.

## OpenCode assistant

Специализированный `opencode` adapter запускает `opencode run --format json` в workspace узла. Prompt передаётся через stdin, stdout читается как NDJSON event stream, stderr остаётся диагностикой. Takt нормализует итоговый текст, Session ID, version, requested/resolved model и usage, но не вмешивается во внутренний tool loop, agents, MCP и работу с файлами OpenCode.

## Control и execution workspace

Workflow definitions, config, commands, Run state, events, locks и artifacts принадлежат control workspace. При `worktree.enabled` node actions получают отдельный execution workspace. Умный router остаётся в control workspace и запускает выбранный процесс отдельным governed child Run; ребёнок применяет собственную worktree policy или явный `isolation`. Это защищает исходный checkout от изменений, но не является sandbox.

## Состояние

Каждый transition записывается через `Store.Commit`. State и event получают одну revision. `Load` обнаруживает рассогласование. `answer` и `resume` получают lock и проверяют fingerprints определений. Usage всех агентных попыток накапливается в состоянии узла и используется evaluation report. Каждый governed child имеет собственный state/events/artifacts и связывается с родителем через явные IDs; approval/resume/cancel проходят по этой связи без объединения файлов состояния.

## Evaluation

`takt eval run` выполняет каждый Markdown-case в отдельной копии workspace template. Перед запуском evaluation вычисляет strategy fingerprint из workflow/config/commands и benchmark fingerprint из набора заданий, копируемого workspace template, quality/generation nodes и валидатора. Предметный validator остаётся частью workflow и возвращает `takt-validation/v1alpha1`; evaluation сохраняет assistant/version/requested/resolved model и агрегирует attempts, duration, usage, approvals, errors, diagnostics и метрики качества.

## Scope безопасности

Текущая архитектура предполагает trusted local input. Наличие path validation для Run ID и process limits не делает runtime безопасным для недоверенных workflow.

## Компиляция композиции

Loader разворачивает `subworkflow` и `foreach` до валидации DAG. Runtime получает обычные nodes и внутренние no-op/result nodes, поэтому отдельного nested scheduler или nested Run store нет. Подключённые definitions входят в fingerprint.

## Governed child execution

Узел `workflow` остаётся в DAG родителя как одна action boundary. Runtime создаёт отдельный Child Run ID и запускает новый Runner с тем же файловым Repository, но другим каталогом Run. Parent node ждёт terminal status ребёнка и получает его output/usage как execution result. При approval родитель хранит waiting link, а CLI продолжает фактического ребёнка и затем parent chain. При retry создаётся новый child Run.

Governed lifecycle не требует сервера или БД: связи сохраняются в локальном RunState, cancellation — в durable marker. Эта модель подходит однопользовательскому локальному runtime, но не заменяет distributed orchestration.
