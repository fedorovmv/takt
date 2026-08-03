# Спецификация `takt/v1alpha1`

## 1. Файловая структура

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

## 2. Конфигурация моделей и исполнителей

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
    argv: ["pi", "--mode", "print", "--model", "{{model.id}}", "{{prompt}}"]
    capabilities: [session_resume, mcp, skills]
```

`process`-адаптер поддерживает шаблоны:

- `{{prompt}}`;
- `{{model.name}}`;
- `{{model.id}}`;
- `{{model.provider}}`;
- `{{model.params}}`;
- `{{workspace}}`;
- `{{session.mode}}`;
- `{{session.id}}`.

Если `{{prompt}}` отсутствует в `argv`, prompt передаётся через stdin.

## 3. Markdown-команды

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

## 4. Workflow

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
    attempts:
      max: 3
    hooks:
      after_node:
        - id: validate
          bash: go test ./...
          on_failure:
            action: retry
            session: fresh

  - id: approve
    depends_on: [implement]
    approval:
      message: Подтвердите результат
      capture_response: true
```

## 5. Типы узлов

### `command`

Загружает Markdown-команду и запускает assistant.

### `prompt`

Передаёт assistant встроенный prompt.

### `bash`

Выполняет команду через `bash -lc`. Stdout становится output узла, stderr сохраняется в журнале. `allow_failure: true` превращает ненулевой код завершения в обычный результат узла; это полезно для проверок внутри циклов.

### `approval`

Переводит Run в `waiting`. Ответ сохраняется как output узла при `capture_response: true`.

### `loop_group`

Повторяет вложенный DAG до выполнения условия или достижения `max_iterations`.

Базовая реализация поддерживает условие:

```yaml
until:
  node: validate
  exit_code: 0
```

Также поддерживается `output_contains`.

## 6. Зависимости и условия

`depends_on` задаёт зависимости DAG.

Поддерживаемые `trigger_rule`:

- `all_success` — значение по умолчанию;
- `all_done`;
- `none_failed_min_one_success`.

Базовый синтаксис `when`:

```yaml
when: nodes.analyze.exit_code == 0
when: nodes.classify.output == "feature"
when: inputs.mode != "dry-run"
```

## 7. Hooks

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

Поддерживаемые действия:

- `continue`;
- `retry`;
- `fail`.

Stdout и stderr неуспешного hook добавляются в `${feedback}` следующей попытки.

## 8. Переменные

Поддерживаются:

- `$USER_MESSAGE`;
- `$ARTIFACTS_DIR`;
- `${input}`;
- `${feedback}`;
- `${nodes.<id>.output}`;
- `${nodes.<id>.exit_code}`;
- `${loop.previous.<id>.output}`;
- `${approvals.<id>}`.

## 9. Состояние

Каждый Run хранится в:

```text
<workspace>/.takt/runs/<run-id>/
  state.json
  events.jsonl
  artifacts/
```

Статусы Run:

- `running`;
- `waiting`;
- `completed`;
- `failed`;
- `cancelled`.

## 10. CLI

```text
takt validate <workflow> --config <config> --workspace <dir>
takt run <workflow> --config <config> --workspace <dir> --input <file-or-text>
takt answer <run-id> <node-id> --workspace <dir> --value <text>
takt status <run-id> --workspace <dir>
takt command run <name> --config <config> --workspace <dir> --input <text>
```

Все команды поддерживают `--json`.

## 11. Ограничения базовой реализации

- готовые DAG-узлы выполняются в детерминированном порядке, без параллельного запуска;
- approval внутри `loop_group` пока запрещён;
- `native_hooks` сохраняются в запросе адаптера, но runtime их не исполняет;
- восстановление выполняется из `state.json`, журнал событий служит для аудита;
- MCP-сервер пока не реализован, кодовый агент запускает CLI через shell/skill.
