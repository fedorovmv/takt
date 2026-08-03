# Спецификация `takt/v1alpha1`

Статус: текущий реализованный внешний контракт `v0.1.6-alpha`. Целевые изменения v0.2 описаны в `08-target-v0.2.md`, `09-runtime-semantics.md` и `10-assistant-adapter-spec.md`. Машиночитаемые схемы находятся в `schemas/`.

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
    type: process
    argv: ["pi", "--mode", "print", "--model", "{{model.id}}"]
    capabilities: [session_resume, mcp, skills]
    protocol: takt-assistant/v1alpha1
    max_output_bytes: 1048576
```

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

При `protocol: takt-assistant/v1alpha1` stdin содержит строгий JSON request envelope, а stdout должен содержать один JSON result envelope. Runtime проверяет версию, тип, статус, `exit_code`, session resume и отсутствие неизвестных полей. Невалидный или обрезанный результат классифицируется как `protocol`, а отказ resume не превращается в fresh. Схема находится в `schemas/assistant-protocol.schema.json`.

Переменные окружения process assistant включают `TAKT_RUN_ID`, `TAKT_NODE_ID`, `TAKT_ATTEMPT`, модель, workspace, session и native hooks.

`max_output_bytes: 0` означает отсутствие лимита. При превышении положительного лимита output обрезается, а NodeState получает `output_truncated: true`.

На Unix процесс запускается в отдельной process group; timeout и cancellation завершают процесс и его потомков.

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

Выполняет команду через `bash -lc`. Stdout и stderr объединяются в output узла.

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

Вложенные `loop_group` и approval внутри `loop_group` не поддерживаются в `v1alpha1`.

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
- `${approvals.<id>}`.

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
- session IDs;
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
```

Все команды поддерживают `--json`; `run`, `answer`, `resume`, `status` и `command run` используют JSON по умолчанию.

## 13. Ограничения

- DAG выполняется последовательно;
- approval и вложенный `loop_group` внутри `loop_group` запрещены;
- `native_hooks` передаются адаптеру, но не исполняются runtime;
- нет `takt cancel`;
- нет sandbox, server, MCP и Web UI;
- stale lock требует ручного удаления после аварийного завершения процесса;
- specialized Pi/OpenCode adapter пока не реализован;
- `takt-assistant/v1alpha1` реализован только для универсального `process`, потоковые события пока не входят в контракт.
