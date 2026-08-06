# Спецификация `takt/v1alpha1`

Статус: текущий реализованный внешний контракт `v0.1.39-alpha`. Целевые изменения v0.2 описаны в `08-target-v0.2.md`, `09-runtime-semantics.md` и `10-assistant-adapter-spec.md`. Машиночитаемые схемы находятся в `schemas/`.

## 1. Область применения

Текущая реализация рассчитана на локальный однопользовательский trusted runtime. Workflow, config, Markdown-команды, shell-команды и рабочая директория считаются доверенными.

## 1.1. Локальный MCP control plane

Команда `takt mcp --surface agent --workspace <dir> [--config <path>]` запускает одноразовый stdio JSON-RPC/MCP adapter поверх общего control service. `takt mcp --daemon` проксирует тот же stdio-протокол в локальный `takt daemon` через Unix socket. БД и сетевой listener не создаются.

Поддерживаются два протокольных входа:

- legacy `initialize` с версиями MCP 2025;
- stateless `server/discover` с `protocolVersion: 2026-07-28`.

Полная совместимая поверхность публикует 53 операции, разделённые на `agent|host|worker|operator|all`. Поверхность `agent` является default и содержит только пять `takt.task.start|status|respond|stop|explain`; host-control, notification delivery и внешний executor/tool-call lifecycle скрыты от основной LLM. `takt.run.start` по умолчанию отсоединяет запуск и возвращает устойчивый `run_id`. События читаются по `revision` cursor, а содержимое артефактов выдаётся только по явному запросу с ограничением размера.

MCP и daemon являются локальными интерфейсами текущего пользователя. Они не добавляют sandbox или новые полномочия и не предназначены для сетевой публикации. Полный control contract зафиксирован в `44-local-mcp-control-plane-v0.1.30.md`, внешний executor и события — в `45-agent-events-external-executor-v0.1.31.md`, а daemon и authoring preflight — в `47-authoring-local-daemon-v0.1.33.md`.

## 1.2. Authoring preflight

`takt validate` проверяет неизвестные поля с path-aware `did you mean`, command/model/assistant references, effective adapter capabilities, статические `${nodes.*}`/approval/artifact references и несовместимые параметры. Diagnostics возвращаются в JSON; `--warnings-as-errors` делает предупреждения ошибками CI.

Renderer использует явные формы: `${path}` — обязательная ссылка, `${path?}` — optional, `${path:-default}` — значение по умолчанию. Неразрешённая обязательная ссылка является ошибкой и не передаётся действию как буквальный текст.

## 1.3. Локальный daemon

`takt daemon start|status|stop` управляет локальным процессом одного workspace. Daemon слушает `.takt/daemon.sock`, использует тот же файловый Store и поддерживает background `takt run --daemon`, `takt events --daemon --follow` и `takt mcp --daemon`. Несколько клиентов одного пользователя сериализуют короткие изменения Run через существующий file lock с bounded retry. Daemon переживает закрытие клиента. После перезапуска он обнаруживает локальные `running|pausing` Run с мёртвым executor PID, помечает незавершённую attempt как `worker_lost`, возвращает узел в `pending` и продолжает граф. Это PID-based recovery и не гарантирует отсутствие повторного внешнего side effect.

## 2. Файловая структура

```text
.takt/
  config.yaml
  commands/
  workflows/
  runs/
  host-sessions/
  notifications.yaml
  notifications/
```

Порядок поиска команд:

1. `<workspace>/.takt/commands/`;
2. `commands/` рядом с workflow;
3. для подключённого или профильного workflow — `commands/` в каждом родительском каталоге до корня композиции/профиля;
4. `~/.takt/commands/`.

## 3. Конфигурация моделей и исполнителей

```yaml
apiVersion: takt/v1alpha1
kind: Config
default_assistant: pi

models:
  large:
    provider: internal-qwen
    id: Qwen3.6-27B
    params:
      reasoning_effort: high

assistants:
  pi:
    type: pi
    binary: pi
    args: [--offline]
    session_dir: .takt/pi-sessions
    project_trust: deny
    capabilities: [session_resume, skills]
    max_output_bytes: 10485760
```

Типы assistant:

- `mock` — детерминированная заглушка;
- `process` — универсальный текстовый или JSON process adapter, включая внешние Codex/Oh My Pi/Qwen CLI wrappers;
- `pi` — специализированный adapter для `earendil-works/pi` через `pi --mode rpc`;
- `opencode` — специализированный JSON CLI adapter OpenCode.

Профиль может ссылаться на логическое имя `coding-agent`; оно разрешается через `default_assistant`. Ядро Takt не зависит от Kiro CLI.

`process`-адаптер поддерживает шаблоны:

- `{{prompt}}`;
- `{{run.id}}`;
- `{{node.id}}`;
- `{{attempt}}`;
- `{{model.name}}`;
- `{{model.id}}`;
- `{{model.provider}}`;
- `{{model.params}}`;
- `{{workspace}}`;
- `{{session.mode}}`;
- `{{session.id}}`.

Если `protocol` не задан и `{{prompt}}` отсутствует в `argv`, prompt передаётся через stdin.

При `protocol: takt-assistant/v1alpha1` stdin содержит строгий JSON request envelope, а stdout — ровно один JSON result envelope. `takt-assistant/v1alpha2` использует тот же request boundary, но stdout является NDJSON event stream с ровно одним terminal result event. Runtime проверяет версию, type, status, обязательный `exit_code`, отсутствие неизвестных полей, неотрицательные usage-метрики и session resume. OS exit code и terminal `result.exit_code` обязаны совпадать всегда, включая ноль; расхождение классифицируется как `protocol`. Невалидный, обрезанный или дополнительный terminal result также является `protocol`, а отказ resume не превращается в fresh. Схема находится в `schemas/assistant-protocol.schema.json`.

Переменные окружения process assistant включают `TAKT_RUN_ID`, `TAKT_NODE_ID`, `TAKT_ATTEMPT`, модель, workspace, session и native hooks.

`max_output_bytes: 0` означает отсутствие лимита. При превышении положительного лимита output обрезается, а NodeState получает `output_truncated: true`.

На Unix процесс запускается в отдельной process group; timeout и cancellation завершают процесс и его потомков.


## 3.0. Входной контракт workflow

Workflow может объявить `input.format: json` и строгую JSON Schema в `input.schema`. До создания Run Takt декодирует вход, отклоняет неизвестные поля и применяет проверяемый subset (`type`, `properties`, `required`, `additionalProperties`, `enum`, `items`, `minItems`/`maxItems`, `uniqueItems`, `minLength`/`maxLength`, `pattern`, `minimum`/`maximum`, `minProperties`/`maxProperties`, integer semantics), общий со structured output. Профиль может задать JSON input отдельно для каждого workflow.

Это используется шестью основными процессами профиля `code` 0.14.0: issue/idea/plan/review/PIV/Ralph входы проверяются до вызова assistant и изменения Git workspace.

## 3.1. Внешний исполнитель AI-узла

`command` и `prompt` поддерживают `executor: external`. Runtime разрешает команду, шаблоны, model/session и effective policy, затем сохраняет durable external task и переводит Run в `waiting` с `kind: external_node`. Worker получает задачу через MCP, заявляет capability declaration и lease. Claim token не входит в публичную проекцию Run.

Event protocol v2 использует: `assistant.session.started`, `assistant.session.resumed`, `assistant.message`, `assistant.tool.requested`, `assistant.tool.allowed`, `assistant.tool.denied`, `assistant.tool.started`, `assistant.tool.completed`, `assistant.artifact.declared`, `assistant.usage`, `assistant.diagnostic`, `assistant.completed`, `assistant.failed`. Raw stdout/stderr сохраняются отдельно.

При `tool_approval` worker обязан запросить tool call до фактического запуска. Takt сначала применяет effective node policy, затем при необходимости сохраняет blocking approval. Запуск разрешён только после `allow`. Отмена одного tool call сохраняется отдельно от отмены Run. Артефакт внешнего worker регистрируется через `takt.node.artifact.declare` и связывается с устойчивым `call_id`. Внешний узел нельзя завершить, пока tool call не достиг terminal-состояния `completed|failed|denied|cancelled`. После `takt.node.complete|fail` результат проходит обычные `output_format`, attempts, hooks и artifact semantics.

Встроенные adapters используют тот же нормализованный event contract через `assistant.Request.Emit`, но заявляют только реально доступные capabilities. Наблюдательные tool events OpenCode/Pi не означают pre-execution `tool_control`.

### Pi assistant

`type: pi` использует официальный RPC-режим Pi. Takt запускает отдельный процесс на попытку узла, запрашивает состояние сессии и накопленную статистику, отправляет prompt через JSONL RPC и ждёт финальное событие `agent_settled`. Событие `agent_end` считается границей одного низкоуровневого запуска и не завершает попытку, если Pi выполняет автоматический retry, compaction retry или queued continuation. После `agent_settled` adapter читает итоговый текст, сообщения, повторную статистику и Session ID. Фактическая модель берётся из `responseModel` последнего assistant message с fallback на выбранную модель сессии; результат version probe также сохраняется в `NodeState`. Затем adapter закрывает stdin для штатного завершения процесса.

Поля конфигурации:

- `binary` — путь к `pi`, по умолчанию `pi` из `PATH`;
- `args` — дополнительные нерезервированные параметры Pi;
- `session_dir` — каталог сессий, передаётся как `--session-dir`;
- `project_trust` — `default`, `approve` или `deny`; последние два соответствуют `--approve` и `--no-approve`;
- `env` — дополнительные переменные окружения;
- `max_output_bytes` — общий лимит RPC stdout и stderr; при нуле используется безопасный лимит adapter по умолчанию. Если timeout или cancellation совпали с переполнением, причина context сохраняет классификацию `timed_out` или `cancelled`, а truncation остаётся диагностическим признаком.

Takt сам задаёт `--mode rpc`, `--provider`, `--model`, `--thinking`, `--session` и параметры trust/session directory. Эти флаги запрещены в `args`, чтобы исключить расхождение структурированного Request и фактического запуска.

`model.provider` и `model.id` должны соответствовать каталогу моделей Pi. Параметры `thinking` или `reasoning_effort` переводятся в `--thinking`; остальные model params доступны расширениям через `TAKT_MODEL_PARAMS_JSON`, но не интерпретируются adapter.

При `session: resume` adapter передаёт `--session <id>` и проверяет через `get_state`, что Pi действительно открыл тот же Session ID. Тихий переход на fresh запрещён. В режиме `fresh` сохранённый ID не передаётся.


Статистика `get_session_stats` является накопленной по всей сессии. Adapter снимает её до prompt и после `agent_settled`, а в `Result.Usage` записывает неотрицательную дельту текущей попытки. Уменьшение накопленных значений или исчезновение usage из второго снимка после его наличия в первом классифицируется как `protocol`. Явные нулевые значения валидны. Полные снимки сохраняются в structured result как `stats_before` и `stats_after`.

`metadata` остаётся необязательным полем внутреннего Request. Текущий workflow runtime его не формирует, однако Pi adapter прозрачно передаёт заполненное значение через `TAKT_METADATA_JSON`. `native_hooks` передаются через `TAKT_NATIVE_HOOKS_JSON`; автоматического преобразования в Pi extensions в `v1alpha1` нет.

Интерактивные запросы Pi extension UI (`confirm`, `select`, `input`, `editor`) отклоняются как `protocol`: Takt approval должен быть отдельным сохраняемым узлом workflow. Fire-and-forget события `notify`, `setStatus`, `setWidget`, `setTitle` и `set_editor_text` допускаются и не требуют ответа.

### OpenCode assistant

`type: opencode` запускает `opencode run --format json` в workspace узла. `binary`, `args`, `agent`, `auto_approve`, `env` и `max_output_bytes` задаются в config. `argv`, `protocol`, `session_dir` и `project_trust` для этого типа запрещены.

Takt передаёт выбранную модель как `<provider>/<id>`, `params.variant` как `--variant`, prompt через stdin, а сохранённый Session ID — через `--session`. При resume возвращённый event stream обязан содержать тот же Session ID. Stdout содержит NDJSON events; stderr остаётся диагностическим. Usage одной попытки является суммой уникальных `step_finish`; event `error` является отказом агента независимо от OS exit code.

При timeout/cancellation итоговая классификация остаётся `timed_out`/`cancelled`. Доступные сообщения о provider retry, соединении и error events сохраняются в raw stdout/stderr, logical output и тексте ошибки узла. Общая проверка attempt context не заменяет более содержательную context-ошибку OpenCode на общее сообщение.

`auto_approve: true` включает OpenCode `--auto` и предназначен только для доверенной рабочей директории.


## Dynamic Takt

Высокоуровневый `takt plan`/`takt.plan` возвращает решение `existing|planned`. `planned` использует `takt/v1alpha1 WorkflowPlan`: цель, жёсткие budgets, упорядоченные фазы `task|map`, зависимости, источник map и явные checkpoint. `uses` обязан ссылаться на разрешённый блок профиля. План проходит строгую проверку и компилируется в обычный `takt/v1alpha1 Workflow`; отдельная runtime-семантика для WorkflowPlan отсутствует.

`takt execute` требует подтверждение planned-плана. Перепланировщик вызывается только после checkpoint и возвращает `continue|replace_remaining|finish|ask_user`. `replace_remaining` создаёт новую revision и не изменяет завершённые фазы. `takt steer` сохраняет уточнение до ближайшего checkpoint. Completed planned-план может быть продвинут через `takt plan promote` в `.takt/workflows/generated/` после повторной загрузки и Validate.

### Доверенные пакеты блоков

Профиль может объявить `block_packages`: список локальных `takt/v1alpha1 BlockPackage`. Каждый пакет содержит уникальные блоки с относительным workflow path, capabilities, integrations, `output_paths`, required checks и policy, а также templates и governance. Governance объединяет required blocks/checks, allowed integrations, branch rules, change-request template и пределы `max_child_runs|max_parallel|max_iterations|max_tokens`.

Начиная с v0.1.39-alpha пакет также может объявлять внутренние `roles`. Блок связывается с ролью и получает bounded `TaskBrief`, context recipe, `expected|allowed|protected|forbidden` scope и `checks` с `required|preferred` + `deny|repair|warn`. Эти роли не являются глобальными агентами кодинг-хоста. Машиночитаемые схемы: `schemas/block-package.schema.json` и `schemas/task-brief.schema.json`.

Каталог загружается до планирования. Workflow блока проходит обычный Load/Validate, обязан иметь один публичный terminal output и не может запускать governed child Run. Каждый `output_path` обязан существовать в terminal `output_format`; источник `map` должен точно совпасть с объявленным путём типа `array`. Общий fingerprint манифестов и workflow сохраняется в плане и проверяется до execute/replan/promote.

CLI: `takt block list|describe|validate`. MCP: `takt.block.list|describe`. Schema: `schemas/block-package.schema.json`.

## 4. Markdown-команды

```markdown
---
description: Исправляет реализацию по результатам проверки
assistant: pi
model: large
---

Исправь проект.

Запрос пользователя:
$USER_MESSAGE

Результат предыдущей проверки:
${feedback}
```

Frontmatter поддерживает `description`, `assistant`, `model`. Остальные поля сохраняются как метаданные.

## 5. Workflow

```yaml
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: example

defaults:
  assistant: pi
  model: large

nodes:
  - id: implement
    command: implement
    timeout: 10m
    attempts:
      max: 3
    hooks:
      after_node:
        - id: validate
          bash: go test ./...
          on_failure:
            action: retry
            session: fresh

  - id: cleanup
    depends_on: [implement]
    trigger_rule: all_done
    bash: rm -f temporary.file

  - id: approve
    depends_on: [implement]
    approval:
      message: Подтвердите результат
      capture_response: true
```

`timeout` использует формат Go duration: `500ms`, `30s`, `5m`, `1h` и ограничивает всю попытку узла. `idle_timeout` поддерживается AI-узлами и сбрасывается нормализованными событиями активности; для claimed внешнего узла его обслуживает daemon. `always_run: true` запускает cleanup-узел после terminal-состояния всех зависимостей независимо от их результата, но не скрывает failure основного графа.

Независимые готовые узлы `command`, `prompt` и `bash` без portable hooks и повторных попыток выполняются одной параллельной волной. Переходы `pending → running → terminal` и запись событий сериализуются, поэтому Run и журнал остаются едиными и детерминированными. Узлы с hooks или `attempts.max > 1` пока исполняются последовательно.

## 6. Типы узлов

### `command`

Загружает Markdown-команду и запускает assistant.

### `prompt`

Передаёт assistant встроенный prompt.

Для `command`, `prompt` и `script` можно задать проверяемый JSON-контракт `output_format`. Runtime принимает ровно одно JSON-значение, проверяет типы, обязательные поля, `enum`, дополнительные свойства, min/max для массивов, строк, чисел и объектов, а также строковый `pattern`, затем сохраняет канонический компактный JSON. Нарушение контракта завершает узел ошибкой `protocol`.

```yaml
- id: classify
  prompt: Классифицируй запрос и верни только JSON.
  output_format:
    type: object
    properties:
      workflow:
        type: string
        enum: [assist, fix-github-issue]
      reason:
        type: string
    required: [workflow, reason]
    additionalProperties: false
```


### Политики `command` и `prompt`

```yaml
- id: classify
  command: classify-change
  allowed_tools: []
  skills: []

- id: review
  command: review-code
  denied_tools: [edit, write]
  skills: [skills/go-review]
  mcp: mcp/repository.json
  sandbox:
    filesystem: read_only
    network: deny
  requires: [tool_policy, skills, mcp]
```

Поля поддерживаются только у `command` и `prompt`:

- `allowed_tools` — верхняя граница доступных инструментов; явный пустой список означает отсутствие инструментов;
- `denied_tools` — дополнительный deny-list; пересечение с allowlist запрещено;
- `skills` — список имён или путей; явный пустой список запрещает inherited skills;
- `mcp` — JSON-файл конфигурации относительно workflow;
- `sandbox.filesystem: read_only` и `sandbox.network: deny` — обязательные гарантии adapter;
- `requires` — дополнительные capability names.

До запуска assistant runtime вычисляет эффективную политику, проверяет `Adapter.Capabilities()`, сохраняет её в `NodeState.policy` и передаёт adapter. Неподдерживаемая capability завершает узел до запуска процесса. Для governed child Run поле `workflow.policy` задаёт inherited upper bound. Allowlist и skills пересекаются, deny/requirements объединяются, а более строгая sandbox-политика наследуется. Файлы MCP и локальные skills входят в fingerprint.

Это assistant-enforced contract, а не OS sandbox. `process` обязан объявить поддерживаемые capabilities в config и получает политику через `takt-assistant/v1alpha1` и `TAKT_POLICY_JSON`. Pi поддерживает tool policy, path skills и read-only tool restriction. OpenCode получает permission/MCP config через `OPENCODE_CONFIG_CONTENT`; локальные path skills дополнительно внедряются в prompt. Network deny не объявляется встроенными Pi/OpenCode и потому отклоняется до запуска.

### `bash`

Выполняет команду через `bash -lc`. Runtime сохраняет stdout и stderr раздельно в `stdout`/`stderr`, а также формирует объединённый `output` для шаблонов, feedback и диагностики.

`allow_failure: true` разрешает только штатный ненулевой exit code. Ошибка запуска, timeout, cancellation или ошибка runtime остаются ошибкой узла.


### `script`

Запускает детерминированный скрипт без assistant:

```yaml
- id: index
  script:
    runtime: command
    path: tools/build-index
    args: [--json]
    env:
      MODE: strict
    dependencies: [schemas/index.schema.json]
  output_format:
    type: object
    properties:
      files:
        type: array
        items: {type: string}
    required: [files]
  output_type: index
  output_mime: application/json
```

`runtime` принимает `command`, `python` или `node`. `command` требует `path`; `python` и `node` принимают ровно одно из `path` и `inline`. Дополнительно доступны `args`, `env`, `working_directory` и `dependencies`. Пути вычисляются относительно workflow и отображаются в execution workspace при managed worktree. Runtime передаёт `TAKT_RUN_ID`, `TAKT_NODE_ID`, `TAKT_ATTEMPT`, `TAKT_WORKSPACE` и `TAKT_ARTIFACTS_DIR`.

Stdout/stderr сохраняются раздельно. `output_format` нормализует только `Output`, не затирая raw stdout. Исходник script и файлы `dependencies` входят в fingerprint.

### Типизированные артефакты

`command`, `prompt`, `bash` и `script` могут объявить `output_type`, `output_mime` и `output_path`. Если `output_path` отсутствует, сохраняется нормализованный `Output`; если указан, файл копируется из execution workspace либо `$TAKT_ARTIFACTS_DIR` в хранилище Run. Ссылка содержит type, MIME, SHA-256, size, producer Run/Node, attempt и timestamp.

```yaml
- id: plan
  command: create-plan
  output_type: plan
  output_mime: text/markdown
  output_path: $ARTIFACTS_DIR/plan.md

- id: implement
  depends_on: [plan]
  prompt: |
    Реализуй план из файла:
    ${nodes.plan.artifacts.plan.path}
```

Доступны `${nodes.<id>.artifacts.<type>.path}`, `.sha256`, `.mime`, `.size`, producer metadata и обращение по числовому индексу. Governed child Run и fan-out поднимают ссылки родителю, сохраняя producer provenance.

### `approval`

Переводит Run и Node в `waiting`. Ответ сохраняется как output узла при `capture_response: true`. Остановка на approval не расходует попытку.

### `loop_group`

Повторяет вложенный DAG до выполнения условия или достижения `max_iterations`. Дочерний DAG использует те же `depends_on`, `when`, `trigger_rule`, hooks и классификацию ошибок, что и корневой workflow.

```yaml
until:
  node: validate
  exit_code: 0
```

Также поддерживается `output_contains`. Условие `until` проверяется только для дочернего узла со статусом `completed`; `skipped`, `failed`, `errored`, `timed_out` и `cancelled` не завершают цикл даже при совпадающем нулевом `exit_code`.

Если timeout или cancellation родительской попытки наступают во время выполнения дочернего узла, родительский `loop_group` и Run сохраняют `timed_out` или `cancelled`. Производная ошибка `loop_group exhausted` не переопределяет причину завершения контекста.

`subworkflow`, `foreach` и `approval` разрешены внутри `loop_group` и используют тот же дочерний DAG. При остановке на approval сохраняется активная итерация; `takt answer` продолжает её, а следующая итерация создаёт новый запрос approval. Поле `until.node` ссылается на публичный ID контейнера. Вложенные `loop_group` остаются запрещены в `v1alpha1`.

### `subworkflow`

Подключает отдельный `takt/v1alpha1 Workflow` и компилирует его в тот же DAG до запуска:

```yaml
- id: implementation
  assistant: opencode
  model: main
  session: resume
  subworkflow:
    path: workflows/implementation.yaml
    inputs:
      plan: ${input}
    output_node: result
```

Путь вычисляется относительно содержащего workflow. В подключённом файле значения доступны как `${inputs.<name>}`. Неразрешённая `${inputs.<name>}` является ошибкой загрузки. Если terminal-узел один, `output_node` выводится автоматически; при нескольких terminal-узлах поле обязательно.

Публичный ID контейнера сохраняется для `depends_on` и `${nodes.<id>.output}`. CLI показывает только публичные узлы. Внутренние namespaced ID с `__` сохраняются в `state.json` для точного resume и проверки определения. Approval внутри подключённого workflow отображается и принимается через публичный ID контейнера.

Локальная Markdown-команда сначала ищется в `commands/` рядом с подключённым workflow, затем в родительских каталогах до корня композиции. Поэтому workflow из `profiles/code/workflows/` использует команды из `profiles/code/commands/`. Содержимое встроенной команды входит в workflow fingerprint.

`assistant`, `model` и `session` на контейнере задают defaults вызова. Приоритет: явное поле дочернего узла → контейнер → defaults дочернего workflow → defaults родительского workflow. Положительный `attempts.max`, непустые `timeout`, hooks и `native_hooks`, а также `allow_failure: true` задаются внутри подключённого workflow. Нулевые и пустые значения этих полей трактуются так же, как отсутствие поля; схема повторяет эту семантику кода.

Рекурсивная ссылка отклоняется с цепочкой файлов. Максимальная глубина развёртывания — 16 одновременно активных workflow; превышение возвращает `subworkflow expansion exceeds depth 16`.

### `workflow` — governed child Run

Запускает подключённый workflow как отдельный Run со своими state, events, artifacts, fingerprints, output и usage:

```yaml
- id: feature
  workflow:
    path: workflows/feature-development.yaml
    input: ${input}
    output_node: summary
    isolation: inherit
    policy:
      denied_tools: [edit, write]
      sandbox:
        filesystem: read_only
```

`path` вычисляется относительно содержащего workflow. `input` проходит обычный renderer. Если `output_node` не задан, в дочернем определении должен быть ровно один terminal-узел. В отличие от `subworkflow`, дочерние узлы не встраиваются в DAG родителя.

Ребёнок хранит `parent_run_id` и `parent_node_id`; родитель — список `child_run_ids`, а узел — текущий `child_run_id` и историю попыток. Failure/cancellation ребёнка определяет результат родительского узла. Retry узла создаёт новый child Run и сохраняет прежнюю попытку.

Режимы `isolation`:

- пусто — собственная `worktree`-политика ребёнка;
- `inherit` — execution workspace родителя без отдельного worktree;
- `worktree` — принудительно отдельный managed worktree;
- `none` — control workspace без worktree.

Approval ребёнка переводит родителя в `waiting` с `kind: child_run`. `takt answer` можно вызвать по корневому Run ID и публичному ID родительского `workflow`-узла; CLI продолжит фактический approval и затем всю parent chain. `takt cancel` распространяет отмену по дереву. Статические child definitions входят в fingerprint родителя; рекурсия отклоняется, глубина ограничена 16.

Для динамического набора детей используется `workflow.fan_out`:

```yaml
- id: reviews
  depends_on: [classify]
  workflow:
    path: workflows/review.yaml
    input: "${reviewer}"
    isolation: inherit
    fan_out:
      items_from: nodes.classify.output.reviewers
      as: reviewer
      max_parallel: 5
      join: all_success
      allow_empty: false
      allow_duplicates: false
```

`items_from` должен указывать на JSON-массив в структурированном output upstream-узла. `max_parallel` по умолчанию равен 1 и ограничен 64. `join` принимает `all_success`, `all_done` или `one_success`. Каждый элемент получает отдельный child Run и устойчивую запись в состоянии; completed-дети переиспользуются при resume, а изменение массива внутри попытки отклоняется. В `input` доступны `${fanout.item}`, `${fanout.index}`, `${fanout.total}` и алиас из `as`. Дубли канонических элементов отклоняются по умолчанию; `allow_duplicates: true` является явным разрешением двойного запуска. Output родительского узла — упорядоченный JSON-массив статусов, outputs, usage и Run ID детей. `all_success` и `one_success` пока ожидают terminal-состояния всей группы и не останавливают оставшиеся children досрочно.

### `foreach`

Выполняет один subworkflow для элементов из workflow или отдельного YAML/JSON-файла. По умолчанию итерации последовательны; `parallel: true` делает их независимыми узлами одной DAG-волны:

```yaml
- id: checks
  foreach:
    as: check
    parallel: true
    items_from:
      path: checks.yaml
    subworkflow:
      path: workflows/check.yaml
      inputs:
        name: ${check}
```

Нужно задать ровно один источник: `items` или `items_from.path`. Путь вычисляется относительно содержащего workflow; файл должен содержать непустой массив верхнего уровня. Его исходные байты входят в fingerprint определения, поэтому изменение списка блокирует resume ранее начатого Run.

Поддерживаются scalar и inline JSON objects. Для объекта доступны `${check}` как JSON и `${check.<field>}`. `${index}` и `${check.index}` содержат индекс с нуля. При `parallel: false` каждая итерация зависит от предыдущей; при `parallel: true` все итерации зависят от общего gate и могут выполняться конкурентно. Публичный output всегда является JSON-массивом результатов в исходном порядке; JSON-результаты сохраняют тип, остальные результаты становятся строками.

Runtime читает только явный массив и не преобразует Markdown-план в task AST.

## 7. Зависимости, ошибки и итог Run

Узел начинает выполнение после terminal-состояния зависимостей.

Поддерживаемые `trigger_rule`:

- `all_success` — все зависимости `completed`;
- `all_done` — любые terminal-состояния зависимостей;
- `none_failed_min_one_success` — нет failure-like зависимостей и есть хотя бы одна `completed`;
- `one_success` — после завершения всех зависимостей запускает узел, если хотя бы одна зависимость `completed`, включая соединение взаимоисключающих ветвей.

После failed/errored/timed_out node scheduler продолжает DAG, чтобы выполнить `all_done`. `always_run` является явной cleanup-семантикой `all_done`; его нельзя совмещать с `when` или другим `trigger_rule`. Итоговый статус Run вычисляется после завершения доступного графа.

Статусы Run:

- `running`;
- `pausing` — оператор запросил безопасную паузу, активные попытки доходят до границы узла;
- `paused`;
- `waiting`;
- `completed`;
- `failed`;
- `cancelled`;
- `abandoned` — оператор завершил обслуживание Run с сохранением истории.

Terminal-состояния Run: `completed|failed|cancelled|abandoned`.

Статусы Node:

- `pending`;
- `running`;
- `waiting`;
- `completed`;
- `failed` — действие штатно завершилось отрицательным результатом, например exit code;
- `errored` — действие не удалось запустить или произошла ошибка runtime;
- `timed_out`;
- `cancelled`;
- `skipped`;
- `blocked`.

Базовый синтаксис `when`:

```yaml
when: nodes.analyze.exit_code == 0
when: nodes.classify.output == "feature"
when: nodes.classify.output.workflow == "fix-github-issue"
when: inputs.input != "dry-run"
```

## 8. Hooks

```yaml
hooks:
  before_node: []
  after_node: []
  before_complete: []
  on_failure: []
```

Hook:

```yaml
- id: validate
  bash: go test ./...
  on_failure:
    action: retry
    session: fresh
```

Timeout и cancellation portable hook относятся ко всей попытке и сохраняют классификацию `timed_out`/`cancelled`; они не преобразуются в обычный `hook_failed`.

Поддерживаемые действия:

- `continue`;
- `retry`;
- `fail`.

Stdout и stderr неуспешного hook добавляются в `${feedback}` следующей попытки.

## 9. YAML subset

Takt не заявляет полную совместимость с YAML 1.2. Поддерживаются карты, списки, простые scalar, inline JSON object, inline list и block scalar:

- `|`, `|-`, `|+`;
- `>`, `>-`, `>+`.

Пустые строки, `#`, `:`, `${...}` и отступы внутри block scalar сохраняются. Tabs запрещены.

## 10. Переменные

Поддерживаются:

- `$USER_MESSAGE`;
- `$ARTIFACTS_DIR`;
- `${input}`;
- `${feedback}`;
- `${nodes.<id>.output}` и `${nodes.<id>.output.<field>}` с вложенными объектами и индексами массивов;
- `${nodes.<id>.exit_code}`;
- `${nodes.<id>.status}`;
- `${loop.previous.<id>.output}`;
- `${approvals.<id>}`;
- `${inputs.<name>}` внутри подключённого subworkflow;
- `${item}`, `${item.<field>}`, `${index}` внутри параметров `foreach` до компиляции.

Неизвестные переменные пока сохраняются как исходный token. Строгий renderer остаётся задачей v0.2.

## 11. Состояние и воспроизводимость

Каждый Run хранится в:

```text
<workspace>/.takt/runs/<run-id>/
  state.json
  events.jsonl
  artifacts/
```

RunState содержит:

- fingerprints workflow, config и Markdown-команд;
- revision;
- статусы и ошибки узлов;
- approval answers;
- session IDs и подтверждённый resume;
- assistant, версию assistant, requested model и resolved model агентных узлов;
- aggregate usage узлов: input/output tokens и cost всех агентных попыток;
- `executions` — отдельные записи фактических попыток с execution identity и usage;
- результаты последней loop iteration;
- parent/child links, run output, aggregate usage и durable cancellation state.

Каждый commit состояния и события получает одну revision. При несовпадении ревизий `Load` возвращает `store_inconsistent`.

`answer` и `resume` используют lock Run и блокируются при изменении определений после старта.

## 12. Профили и именованные workflow

`Profile` задаёт default workflow, config и необязательную карту именованных процессов:

```yaml
apiVersion: takt/v1alpha1
kind: Profile
metadata:
  name: code
workflow: workflow.yaml
workflows:
  assist: workflows/assist.yaml
  piv-loop: workflows/piv-loop.yaml
config: ../../config.yaml
```

Селектор `code` запускает default workflow, например умный роутер. Селектор `code:piv-loop` выбирает именованный файл без роутинга. Имена не могут содержать `:`; пути вычисляются относительно `profile.yaml`.

## 13. JSON CLI

Успех:

```json
{
  "ok": true,
  "result": {}
}
```

Ошибка:

```json
{
  "ok": false,
  "error": {
    "code": "start",
    "message": "...",
    "retryable": false,
    "details": {
      "run_id": "...",
      "node_id": "..."
    }
  }
}
```

Команды:

```text
takt validate <workflow> --config <config> --workspace <dir>
takt run <workflow> --config <config> --workspace <dir> --input <file-or-text>
takt answer <run-id> <node-id> --workspace <dir> --value <text>
takt resume <run-id> --workspace <dir>
takt status <run-id> --workspace <dir>
takt children <run-id> --workspace <dir>
takt cancel <run-id> --workspace <dir> [--reason <text>]
takt command run <name> --config <config> --workspace <dir> --input <text>
takt workflow list <profile> --workspace <dir>
takt workflow describe <profile[:workflow]> --workspace <dir>
takt worktree list --workspace <dir>
takt worktree remove <run-id> --workspace <dir> [--force]
takt worktree prune --workspace <dir>
takt eval run <workflow> --config <config> --cases <dir> --workspace-template <dir> --output <dir> [--strategy-id <id>] [--benchmark-id <id>] [--quality-node <id>] [--generation-node <id>] [--validator-path <path>]
takt eval report <evaluation-output-dir>
```

Все команды поддерживают `--json`; `run`, `answer`, `resume`, `status`, `children`, `artifacts`, `cancel`, `command run` и `eval` используют JSON по умолчанию.

`eval run` выполняет preflight до создания output: нормализованные `case_id` должны быть уникальны, а `workspace-template` и `output` не могут совпадать или быть вложены друг в друга, включая пути через символические ссылки. До запуска вычисляются fingerprints workflow, config, Markdown-команд, упорядоченного набора заданий, копируемого workspace template и указанного валидатора.

`report.json` использует `takt-evaluation/v1alpha1` и сохраняет strategy/benchmark identity, версию Takt и Go-окружение, assistant и его версию, requested model, фактический Pi `responseModel`, attempts, duration, usage, approval answers, statuses, resume, feedback, ошибки узлов и диагностический вывод.

Каждая фактическая попытка действия сохраняется в `nodes.<id>.executions`. Summary группирует tokens/cost по `usage_by_execution_identity`; при смене assistant, его версии, requested или resolved model узел получает `mixed_execution_identity: true`.

При заданном `--quality-node` Takt декодирует доступный строгий `takt-validation/v1alpha1` только из stdout узла и независимо от exit code и terminal status. Stderr сохраняется отдельно и входит в объединённый диагностический output, но не участвует в декодировании envelope. `score`, `checks` и diagnostics сохраняются и участвуют в предметных агрегатах даже для `valid: false` с ненулевым exit code. Успех определяется только сочетанием `quality_node_status: completed` и `quality.valid: true`; результат из failed/errored/timed_out/cancelled/skipped/blocked узла не повышает success rate. Malformed envelope при любом статусе является ошибкой измерительного контура. Runner агрегирует `success_at_1`, итоговую долю корректных результатов, среднюю оценку, попытки до успеха, стоимость и `amortized_end_to_end_ms_per_valid`, а также diagnostics по severity/code.

Измеренные нулевые доли сериализуются как `0`. Метрики, которые нельзя вычислить, например average score без score или cost per valid без корректных результатов, сериализуются как `null`. Общий benchmark fingerprint включает ID, версию и fingerprint валидатора. Workflow и предметный валидатор остаются источником критерия качества; Takt не интерпретирует семантику Route DSL.

## 14. Ограничения

- параллельная волна не включает узлы с portable hooks или `attempts.max > 1`;
- вложенный `loop_group` внутри `loop_group` запрещён;
- `native_hooks` передаются адаптеру, но не исполняются runtime;
- несколько `workflow`-узлов пока не выполняются одной параллельной волной;
- нет OS sandbox для недоверенного кода; filesystem/network policy остаётся assistant-enforced, а server, Web UI и БД — proposal вне локального режима;
- stale lock требует ручного удаления после аварийного завершения процесса;
- специализированные Pi и OpenCode adapters реализованы;
- `takt-assistant/v1alpha1` реализован для универсального `process`; специализированный `pi` использует официальный Pi RPC JSONL, а `opencode` — официальный `run --format json` event stream; потоковые события пока не публикуются в EventSink.


## Managed worktree policy

```yaml
worktree:
  enabled: true
  base: HEAD
  branch_prefix: takt
  cleanup: on_success
  allow_dirty: false
```

State and artifacts remain in the control workspace. Node execution moves into the worktree. `cleanup` is `on_success` or `manual`. Automatic cleanup applies only to a clean successful worktree; an unchanged branch is deleted, while a branch with commits and all states that may contain evidence or changes are retained. `--no-worktree`, `--keep-worktree`, `--worktree-base`, and `--allow-dirty-worktree` override policy and are persisted for resume.
