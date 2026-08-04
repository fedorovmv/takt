# Workflow Takt

## Каркас

```yaml
apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: example
  description: Пример процесса

defaults:
  assistant: pi
  model: main
  session: resume

nodes:
  - id: implement
    prompt: |
      Выполни запрос:
      ${input}
```

## Общие поля узла

```yaml
- id: node-id
  depends_on: [previous]
  when: nodes.previous.exit_code == 0
  trigger_rule: all_success
  assistant: pi
  model: main
  session: resume
  attempts:
    max: 3
  allow_failure: false
  timeout: 20m
```

Поддерживаемые `trigger_rule`:

- `all_success` — все зависимости `completed`;
- `all_done` — любые terminal-состояния;
- `none_failed_min_one_success` — нет failure-like зависимостей и есть хотя бы одна успешная.

## Типы узлов

### Inline prompt

```yaml
- id: implement
  prompt: |
    Выполни запрос ${input}.
    Замечания: ${feedback}
```

### Markdown-команда

```yaml
- id: implement
  command: implement
```

Команда ищется в:

1. `<workspace>/.takt/commands/`;
2. `commands/` рядом с workflow;
3. `~/.takt/commands/`.

### Bash

```yaml
- id: test
  depends_on: [implement]
  bash: go test ./...
```

### Approval

```yaml
- id: approve
  depends_on: [test]
  approval:
    message: Подтвердите результат
    capture_response: true
```

### Loop group

```yaml
- id: improve
  loop_group:
    max_iterations: 3
    nodes:
      - id: edit
        prompt: Исправь решение
      - id: validate
        depends_on: [edit]
        bash: ./validate.sh
    until:
      node: validate
      exit_code: 0
```

Approval и вложенный `loop_group` внутри `loop_group` не поддерживаются.

## Hooks

```yaml
hooks:
  before_node: []
  after_node: []
  before_complete: []
  on_failure: []
```

Пример retry после проверки:

```yaml
- id: implement
  command: implement
  attempts:
    max: 3
  hooks:
    after_node:
      - id: validate
        bash: ./validate.sh
        on_failure:
          action: retry
          session: resume
```

Действия hook: `continue`, `retry`, `fail`.

## Переменные

- `$USER_MESSAGE` и `${input}` — вход пользователя;
- `${feedback}` — вывод неуспешных hooks прошлой попытки;
- `${nodes.<id>.output}`;
- `${nodes.<id>.exit_code}`;
- `${nodes.<id>.status}`;
- `${loop.previous.<id>.output}`;
- `${approvals.<id>}`;
- `$ARTIFACTS_DIR` — каталог артефактов текущего Run.

Неизвестная переменная пока остаётся исходным token, поэтому опечатка может пройти незаметно. Проверяй имена вручную.

## Статусы

`pending`, `running`, `waiting`, `completed`, `failed`, `errored`, `timed_out`, `cancelled`, `skipped`, `blocked`.

`allow_failure: true` действует только на штатный ненулевой exit code.
