# Спецификация `takt/v1alpha1`

Статус: текущий реализованный внешний контракт `v0.1.23-alpha`. Целевые изменения v0.2 описаны в `08-target-v0.2.md`, `09-runtime-semantics.md` и `10-assistant-adapter-spec.md`. Машиночитаемые схемы находятся в `schemas/`.

## 1. Область применения

Текущая реализация рассчитана на локальный однопользовательский trusted runtime. Workflow, config, Markdown-команды, shell-команды и рабочая директория считаются доверенными.

## 2. Файловая структура

```text
.takt/
  config.yaml
  commands/
  workflows/
  runs/
```

Порядок поиска команд:

1. `<workspace>/.takt/commands/`;
2. каталог рядом с workflow: `commands/`;
3. `~/.takt/commands/`.

## 3. Конфигурация моделей и исполнителей

```yaml
apiVersion: takt/v1alpha1
kind: Config

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
- `process` — универсальный текстовый или JSON process adapter;
- `pi` — специализированный adapter для `earendil-works/pi` через `pi --mode rpc`.

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

При `protocol: takt-assistant/v1alpha1` stdin содержит строгий JSON request envelope, а stdout должен содержать ровно один JSON result envelope. Runtime проверяет версию, type, status, обязательный `exit_code`, отсутствие неизвестных полей, неотрицательные usage-метрики и session resume. OS exit code и `result.exit_code` обязаны совпадать всегда, включая ноль; любое расхождение классифицируется как `protocol`. Невалидный, обрезанный или дополнительный JSON также является `protocol`, а отказ resume не превращается в fresh. Схема находится в `schemas/assistant-protocol.schema.json`.

Переменные окружения process assistant включают `TAKT_RUN_ID`, `TAKT_NODE_ID`, `TAKT_ATTEMPT`, модель, workspace, session и native hooks.

`max_output_bytes: 0` означает отсутствие лимита. При превышении положительного лимита output обрезается, а NodeState получает `output_truncated: true`.

На Unix процесс запускается в отдельной process group; timeout и cancellation завершают процесс и его потомков.


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

`timeout` использует формат Go duration: `500ms`, `30s`, `5m`, `1h`. Лимит действует на всю попытку узла: `before_node`, действие, `on_failure`, `after_node` и `before_complete`.

## 6. Типы узлов

### `command`

Загружает Markdown-команду и запускает assistant.

### `prompt`

Передаёт assistant встроенный prompt.

### `bash`

Выполняет команду через `bash -lc`. Runtime сохраняет stdout и stderr раздельно в `stdout`/`stderr`, а также формирует объединённый `output` для шаблонов, feedback и диагностики.

`allow_failure: true` разрешает только штатный ненулевой exit code. Ошибка запуска, timeout, cancellation или ошибка runtime остаются ошибкой узла.

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

`subworkflow` и `foreach` разрешены внутри `loop_group` и компилируются в тот же дочерний DAG. Поле `until.node` ссылается на публичный ID контейнера. Вложенные `loop_group` и approval внутри `loop_group` остаются запрещены в `v1alpha1`.

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

### `foreach`

Последовательно выполняет один subworkflow для элементов из workflow или отдельного YAML/JSON-файла:

```yaml
- id: checks
  foreach:
    as: check
    items_from:
      path: checks.yaml
    subworkflow:
      path: workflows/check.yaml
      inputs:
        name: ${check}
```

Нужно задать ровно один источник: `items` или `items_from.path`. Путь вычисляется относительно содержащего workflow; файл должен содержать непустой массив верхнего уровня. Его исходные байты входят в fingerprint определения, поэтому изменение списка блокирует resume ранее начатого Run.

Поддерживаются scalar и inline JSON objects. Для объекта доступны `${check}` как JSON и `${check.<field>}`. `${index}` и `${check.index}` содержат индекс с нуля. Итерации выполняются строго последовательно. Публичный output — JSON-массив результатов всех итераций в исходном порядке; JSON-результаты сохраняют тип, остальные результаты становятся строками.

Runtime читает только явный массив и не преобразует Markdown-план в task AST. Параллельный режим `foreach` в текущем контракте отсутствует.

## 7. Зависимости, ошибки и итог Run

Узел начинает выполнение после terminal-состояния зависимостей.

Поддерживаемые `trigger_rule`:

- `all_success` — все зависимости `completed`;
- `all_done` — любые terminal-состояния зависимостей;
- `none_failed_min_one_success` — нет failure-like зависимостей и есть хотя бы одна `completed`.

После failed/errored/timed_out node scheduler продолжает DAG, чтобы выполнить `all_done`. Итоговый статус Run вычисляется после завершения доступного графа.

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
- `${nodes.<id>.output}`;
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
- результаты последней loop iteration.

Каждый commit состояния и события получает одну revision. При несовпадении ревизий `Load` возвращает `store_inconsistent`.

`answer` и `resume` используют lock Run и блокируются при изменении определений после старта.

## 12. JSON CLI

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
takt command run <name> --config <config> --workspace <dir> --input <text>
takt eval run <workflow> --config <config> --cases <dir> --workspace-template <dir> --output <dir> [--strategy-id <id>] [--benchmark-id <id>] [--quality-node <id>] [--generation-node <id>] [--validator-path <path>]
takt eval report <evaluation-output-dir>
```

Все команды поддерживают `--json`; `run`, `answer`, `resume`, `status`, `command run` и `eval` используют JSON по умолчанию.

`eval run` выполняет preflight до создания output: нормализованные `case_id` должны быть уникальны, а `workspace-template` и `output` не могут совпадать или быть вложены друг в друга, включая пути через символические ссылки. До запуска вычисляются fingerprints workflow, config, Markdown-команд, упорядоченного набора заданий, копируемого workspace template и указанного валидатора.

`report.json` использует `takt-evaluation/v1alpha1` и сохраняет strategy/benchmark identity, версию Takt и Go-окружение, assistant и его версию, requested model, фактический Pi `responseModel`, attempts, duration, usage, approval answers, statuses, resume, feedback, ошибки узлов и диагностический вывод.

Каждая фактическая попытка действия сохраняется в `nodes.<id>.executions`. Summary группирует tokens/cost по `usage_by_execution_identity`; при смене assistant, его версии, requested или resolved model узел получает `mixed_execution_identity: true`.

При заданном `--quality-node` Takt декодирует доступный строгий `takt-validation/v1alpha1` только из stdout узла и независимо от exit code и terminal status. Stderr сохраняется отдельно и входит в объединённый диагностический output, но не участвует в декодировании envelope. `score`, `checks` и diagnostics сохраняются и участвуют в предметных агрегатах даже для `valid: false` с ненулевым exit code. Успех определяется только сочетанием `quality_node_status: completed` и `quality.valid: true`; результат из failed/errored/timed_out/cancelled/skipped/blocked узла не повышает success rate. Malformed envelope при любом статусе является ошибкой измерительного контура. Runner агрегирует `success_at_1`, итоговую долю корректных результатов, среднюю оценку, попытки до успеха, стоимость и `amortized_end_to_end_ms_per_valid`, а также diagnostics по severity/code.

Измеренные нулевые доли сериализуются как `0`. Метрики, которые нельзя вычислить, например average score без score или cost per valid без корректных результатов, сериализуются как `null`. Общий benchmark fingerprint включает ID, версию и fingerprint валидатора. Workflow и предметный валидатор остаются источником критерия качества; Takt не интерпретирует семантику Route DSL.

## 13. Ограничения

- DAG выполняется последовательно;
- approval и вложенный `loop_group` внутри `loop_group` запрещены;
- `native_hooks` передаются адаптеру, но не исполняются runtime;
- нет `takt cancel`;
- нет sandbox, server, MCP и Web UI;
- stale lock требует ручного удаления после аварийного завершения процесса;
- специализированные Pi и OpenCode adapters реализованы;
- `takt-assistant/v1alpha1` реализован для универсального `process`; специализированный `pi` использует официальный Pi RPC JSONL, а `opencode` — официальный `run --format json` event stream; потоковые события пока не публикуются в EventSink.
