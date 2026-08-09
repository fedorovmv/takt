# Архитектура

```text
CLI / MCP / Local Daemon
 │
 ▼
Transport adapters ──► Stable application services ──► Runtime / Workflow / Run Store
 │                           │
 │                           ├── Extensions: packages / blocks / notifications / adapters
 │                           ├── Experimental: Dynamic Flow / host control / learning
 │                           └── Tooling: evaluation / compatibility
 │
 ▼
Bootstrap / composition root
 │
 ├── Assistant / Domain Adapter / Task Source SDKs
 ├── Git Worktree / Execution Workspace
 ├── Definition fingerprints / revisioned state & events
 └── Artifact store

Dependency rule:
experimental / extensions / tooling ──► stable core
stable core ──X─► experimental / extensions / tooling
```

## Архитектурные контракты эволюции

После стабилизации `v0.1.52–v0.1.57` Takt фиксирует три дополнительные защиты от повторного разрастания связности. Они адаптированы из сильных практик Archon, но встроены в собственную архитектуру Takt без глобальных registries и нового framework-слоя.

### 1. Конституция языка workflow

> **YAML координирует. Код вычисляет. Агент принимает решения.**

Workflow YAML содержит только то, что runtime должен статически видеть для управления Run: структуру, зависимости, gates, retry, approval, session/artifact identity и композицию. Вычисление значения принадлежит `bash`/`script`/`command`, модельное решение — `prompt`/assistant node.

Перед добавлением YAML-поля, node surface или expression capability нужно ответить на три вопроса:

1. Должен ли runtime видеть это для управления Run?
2. Это декларативные данные с фиксированной семантикой, а не вычисление?
3. Можно ли выразить задачу существующим script/command/prompt node и текущей связкой узлов?

Положительный ответ на третий вопрос означает, что новое поле допускается только при отдельной governance-ценности: наблюдаемости, resumability, auditability или безопасном управлении жизненным циклом.

`when` — намеренно малый gate language: только `==`, `!=`, `&&`, `||`, `nodes.*`/`inputs.*` paths и литералы. Скобки, функции, арифметика, regex и операторы порядка не добавляются инкрементально. Более сложное решение вычисляется отдельным узлом и публикуется как structured output. Если когда-либо понадобится полноценный expression language, Takt принимает зрелый специфицированный язык целиком отдельным versioned change, а не выращивает собственный parser. `internal/whenexpr` является единственной реализацией; loader и runtime используют один контракт.

Load-time композиция разворачивается до обычного DAG. Runtime-resolved работа оформляется governed child Run с собственным state/events/artifacts. Experimental Dynamic Flow использует stable runtime, но не расширяет неявно язык workflow.

### 2. Registration descriptors расширений

Concrete assistant integrations объявляют `assistant.ProviderRegistration` (`ID`, display name, stage, factory, version probe). Extension package только декларирует registration; `init()`-регистрация и package-global mutable registry запрещены. Единственный production registry собирается в `internal/bootstrap`, копирует descriptors, проверяет дубликаты и после construction предоставляет только read-only lookup/snapshots.

Stable `internal/assistant` знает registration contract, но не импортирует Pi/OpenCode. Runtime и tooling получают один и тот же собранный provider graph. Таким образом, доступные providers определяются явным object graph, а не порядком скрытых вызовов `register...()`.

### 3. Schema-first canonical operations

`internal/appapi.OperationDescriptor` является единственным источником canonical operation ID, stability stage, MCP tool name, title/description, `InputSchema` и annotations. `registerOperation[T]` связывает descriptor с конкретным Go request type. Вход сначала проходит JSON Schema descriptor, затем strict typed decode.

MCP canonical tools проецируются из `appapi.CanonicalOperations()`, а `docs/71-canonical-operation-contracts.generated.md` генерируется из тех же descriptors и проверяется на drift. Локальные MCP schemas остаются только у MCP-specific external-worker protocol tools, которые не являются canonical application operations.

Эти правила и их executable gates подробно описаны в `docs/72-architecture-contracts-v0.1.57.md` и ADR-090.

## Пакеты

- `internal/yamlcodec` — тонкий Takt-адаптер над `go.yaml.in/yaml/v3`: единый YAML/JSON decode path, `json`-имена полей и strict unknown-field diagnostics; общую YAML grammar Takt не реализует;
- `internal/spec` — структуры текущей схемы;
- `internal/config` — модели и исполнители;
- `internal/command` — Markdown-команды;
- `internal/execution` — классы ошибок и управление process group;
- `internal/diagnostic` — нормализация runtime-ошибок и стабильные SHA-256 fingerprints;
- `internal/redact` — `secret://ENV_NAME`, регистрация известных секретов и редактирование сохраняемых данных;
- `internal/localsandbox` — локальный OS enforcement для deterministic `bash/script` узлов через bubblewrap/sandbox-exec;
- `internal/gitworktree` — создание ветки/worktree, проверка dirty state, удаление и prune;
- `internal/assistant` — stable provider-neutral `mock`/`process` contracts и immutable provider registry; concrete Pi/OpenCode implementations находятся в extensions;
- `internal/definition` — fingerprints workflow/config/commands;
- `internal/workflow` — загрузка и статическая проверка DAG;
- `internal/authoring` — diagnostics для templates, output/artifact references и несовместимых параметров;
- `internal/application` — стабильные use cases Run/Catalog/Authoring/Worktree/Command; внешний worker/tool lifecycle находится в `internal/externalworker`, пакет не зависит от experimental/extensions/tooling;
- `internal/runtime` — общий scheduler, hooks, loops, approval, governed child lifecycle, cancellation tree и итог Run;
- `internal/store` — revisioned state/event store, parent/child links, durable control markers, aggregate usage и Run lock;
- `internal/extensions` — Package Distribution, Block Catalog, Notifications и подключаемые application facades;
- `internal/experimental` — Dynamic Flow/Router/Task/Evidence, Host Control, Learning и связанный workspace catalog;
- `internal/tooling` — evaluation/benchmark и compatibility/schema/field audit;
- `internal/catalogload` — extension-aware сборка каталога профиля и установленных BlockPackage без обратной зависимости `profile -> packages`;
- `internal/maintenance` — orchestration периодических plan/external/notification задач поверх модулей;
- `internal/domainadapter` и `sdk/domainadapter` — нейтральные SCM/tracker/CI operations и process/MCP transports;
- `internal/daemon` — локальный Unix-socket transport и lifecycle host; бизнес-семантика остаётся в modular services;
- `internal/bootstrap` — единственный production composition root, который связывает stable, extensions, experimental и tooling.

## Граница с кодовым агентом

Takt не реализует собственный tool loop. Встроенные adapters публикуют нормализованные session/message/tool/usage/diagnostic events в пределах доступных возможностей. Полный блокирующий tool lifecycle обеспечивают controllable process/external executors; OpenCode/Pi остаются наблюдательными интеграциями.

## Семантика выполнения

Failure узла не завершает Run немедленно. Node становится terminal, после чего scheduler выполняет разрешённые ветви, включая `all_done`. Корневой workflow и дочерний DAG `loop_group` используют один механизм. Завершение attempt context имеет приоритет над производными ошибками контейнера, поэтому timeout/cancellation дочерней работы сохраняются на родительском `loop_group`. Retry/backoff хранит точный `not_before` в RunState; одинаковые ошибки получают нормализованный diagnostic fingerprint. Fan-out `one_success|all_success` может завершить волну досрочно и помечает ненужных siblings причиной `fanout_result_decided`.

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


## Pi assistant extension

Специализированный `pi` adapter запускает `pi --mode rpc`, передаёт prompt по JSONL и получает Session ID, итоговый текст, статистику и сообщения через RPC-команды. Версия Pi сохраняется из version probe, а фактическая модель определяется по `responseModel` последнего assistant message с fallback на выбранную модель сессии. `fresh` создаёт новую сессию, `resume` передаёт сохранённый ID через `--session` и проверяет его по `get_state`. Инструменты, файлы, shell, skills и история остаются внутри Pi.

## OpenCode assistant extension

Специализированный `opencode` adapter запускает `opencode run --format json` в workspace узла. Prompt передаётся через stdin, stdout читается как NDJSON event stream, stderr остаётся диагностикой. Takt нормализует итоговый текст, Session ID, version, requested/resolved model и usage, но не вмешивается во внутренний tool loop, agents, MCP и работу с файлами OpenCode.

## Control и execution workspace

Workflow definitions, config, commands, Run state, events, locks и artifacts принадлежат control workspace. При `worktree.enabled` node actions получают отдельный execution workspace. Умный router остаётся в control workspace и запускает выбранный процесс отдельным governed child Run; ребёнок применяет собственную worktree policy или явный `isolation`. Это защищает исходный checkout от изменений, но само по себе не является sandbox. Для deterministic `bash/script` узлов `sandbox.enforcement: required|optional` может добавить отдельный OS enforcement слой; assistant nodes по-прежнему зависят от capabilities/policy кодинг-агента.

## Script runtime и артефакты

`script`-узел исполняется тем же scheduler и execution context, что `bash`, но запускает явный command/Python/Node/Go source без assistant. Source и dependencies входят в definition fingerprint. После успешного действия `output_type` фиксирует Output либо файл как снимок в Run store с SHA-256 и producer metadata. Перед persistence известные секреты редактируются в текстовых artifacts; бинарный artifact с известным секретом отклоняется. Ссылки governed children агрегируются родителем, но физический файл остаётся у producer Run.

## Состояние

Каждый transition записывается через `Store.Commit`. Перед записью runtime создаёт redacted durable copy; живое in-memory состояние может использовать resolved secret только в пределах текущего исполнения, а CLI/MCP/control после выполнения повторно загружают состояние из Store. State и event получают одну revision. `Load` обнаруживает рассогласование. `answer` и `resume` получают lock и проверяют fingerprints определений. Usage всех агентных попыток накапливается в состоянии узла и используется evaluation report. Каждый governed child имеет собственный state/events/artifacts и связывается с родителем через явные IDs; approval/resume/cancel проходят по этой связи без объединения файлов состояния.

## Evaluation

`takt eval run` выполняет каждый Markdown-case в отдельной копии workspace template. Перед запуском evaluation вычисляет strategy fingerprint из workflow/config/commands и benchmark fingerprint из набора заданий, копируемого workspace template, quality/generation nodes и валидатора. Предметный validator остаётся частью workflow и возвращает `takt-validation/v1alpha1`; evaluation сохраняет assistant/version/requested/resolved model и агрегирует attempts, duration, usage, approvals, errors, diagnostics и метрики качества.

## Authoring и локальный daemon

Authoring preflight выполняется до создания Run: loader предлагает `did you mean`, capability validator проверяет фактические adapters, а analyzer проверяет template/output/artifact references. Renderer остаётся частью runtime и повторяет fail-closed проверку при фактическом выполнении.

`takt daemon` использует те же modular application services и canonical API registry, что CLI/MCP, и добавляет только время жизни процесса и Unix-socket transport. Store, scheduler и модель Run не дублируются. Background Run переживает закрытие клиента, event stream использует revision cursor, а concurrent control mutations сериализуются per-Run lock. БД не требуется.

## Scope безопасности

Текущая архитектура предполагает trusted local input. SecretRef/redaction и локальный OS sandbox уменьшают риск утечки/лишних полномочий, но не создают multi-user или untrusted security boundary. Подробная граница описана в `SECURITY.md`.

## Компиляция композиции

Loader разворачивает `subworkflow` и `foreach` до валидации DAG. Runtime получает обычные nodes и внутренние no-op/result nodes, поэтому отдельного nested scheduler или nested Run store нет. Подключённые definitions входят в fingerprint.

## Governed child execution

Узел `workflow` остаётся в DAG родителя как одна action boundary. Runtime создаёт отдельный Child Run ID и запускает новый Runner с тем же файловым Repository, но другим каталогом Run. Parent node ждёт terminal status ребёнка и получает его output/usage как execution result. При approval родитель хранит waiting link, а CLI продолжает фактического ребёнка и затем parent chain. При retry failed/cancelled child создаётся новый Run; уже completed child переиспользуется, а повторяются только post-child проверки родительского узла. Это предотвращает повторную мутацию успешно завершённого repository child.

Governed lifecycle не требует сервера или БД: связи сохраняются в локальном RunState, cancellation — в durable marker. Эта модель подходит однопользовательскому локальному runtime, но не заменяет distributed orchestration.

## Policy preflight

Перед AI action runtime разрешает локальные MCP/skill paths, объединяет node policy с inherited child policy и сравнивает необходимый набор capabilities с `Adapter.Capabilities()`. Только после успешного preflight создаётся процесс assistant. Applied policy записывается в NodeState и передаётся adapter. Definition fingerprint включает policy files/directories.
