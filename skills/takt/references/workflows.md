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
- `none_failed_min_one_success` — нет failure-like зависимостей и есть хотя бы одна успешная;
- `one_success` — после завершения всех зависимостей есть хотя бы одна успешная ветвь.

## Git worktree isolation

```yaml
worktree:
  enabled: true
  base: HEAD
  branch_prefix: takt
  cleanup: on_success
```

Изоляция применяется ко всему workflow. У подключённого subworkflow policy активируется на его gate до запуска дочерних узлов. Состояние и artifacts остаются в control workspace. Runtime автоматически удаляет только чистый успешный worktree; грязный или неуспешный сохраняется. Это не sandbox.

## Планирование

Независимые готовые `command`, `prompt` и `bash` без portable hooks и повторных попыток выполняются параллельной волной. Узлы с hooks или `attempts.max > 1` пока идут последовательным путём. Ошибка одной ветви не отменяет остальные уже запущенные ветви.

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
3. родительские `commands/` до корня композиции или профиля;
4. `~/.takt/commands/`.

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

Approval, `subworkflow` и `foreach` разрешены внутри `loop_group`; `until.node` использует публичный ID контейнера. Approval сохраняет активную итерацию и после ответа продолжает её. Вложенный `loop_group` остаётся запрещён.

## Проверяемый JSON output

Для `command` и `prompt` доступен `output_format`:

```yaml
- id: classify
  prompt: Верни только JSON.
  output_format:
    type: object
    properties:
      kind:
        type: string
        enum: [bug, feature]
    required: [kind]
    additionalProperties: false
```

Поддерживаются `object`, `array`, `string`, `boolean`, `number`, `integer`, `properties`, `required`, `enum`, `items`, `additionalProperties`. Нарушение контракта является `protocol`-ошибкой. Для retry укажи `attempts.retry_on: [protocol]`; точная ошибка попадёт в `${feedback}`, а raw stdout останется в состоянии.

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
- `${nodes.<id>.output}` и `${nodes.<id>.output.<field>}`;
- `${nodes.<id>.exit_code}`;
- `${nodes.<id>.status}`;
- `${loop.previous.<id>.output}`;
- `${approvals.<id>}`;
- `$ARTIFACTS_DIR` — каталог артефактов текущего Run.

Неизвестная переменная пока остаётся исходным token, поэтому опечатка может пройти незаметно. Проверяй имена вручную.

## Статусы

`pending`, `running`, `waiting`, `completed`, `failed`, `errored`, `timed_out`, `cancelled`, `skipped`, `blocked`.

`allow_failure: true` действует только на штатный ненулевой exit code.

## Subworkflow

`subworkflow` подключает отдельный `takt/v1alpha1 Workflow` и компилирует его в тот же DAG до запуска:

```yaml
- id: implement
  subworkflow:
    path: workflows/implement.yaml
    inputs:
      request: ${input}
    output_node: result
```

В подключённом workflow доступны `${inputs.request}` и другие явно переданные значения. Если terminal-узел один, `output_node` можно не указывать. При нескольких terminal-узлах поле обязательно.

Публичный ID контейнера сохраняется: следующий узел может использовать `depends_on: [implement]` и `${nodes.implement.output}`. CLI скрывает namespaced узлы с `__`; полное состояние хранится на диске для resume. Approval принимается через публичный ID контейнера.

Контейнер поддерживает `id`, `depends_on`, `when`, `trigger_rule`, `assistant`, `model` и `session`. Последние три поля задают defaults дочернего вызова. Положительный `attempts.max`, непустые timeout/hooks/native_hooks и `allow_failure: true` задаются внутри подключённого workflow.

Глубина композиции ограничена 16. Рекурсивные ссылки отклоняются при загрузке. Локальная команда ищется рядом с дочерним workflow и далее вверх до корня композиции.

## Foreach

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

При `parallel: false` элементы выполняются строго по порядку; при `parallel: true` итерации становятся независимыми DAG-ветвями. Поддерживаются строки, числа, логические значения и inline JSON objects. Для объекта доступны `${check}` как JSON и `${check.<field>}` для полей. `${index}` и `${check.index}` содержат индекс с нуля.

`foreach` принимает ровно один источник: inline `items` или `items_from.path` к непустому YAML/JSON-массиву. Результат — JSON-массив outputs всех итераций в исходном порядке, независимо от порядка завершения параллельных ветвей. Содержимое внешнего файла входит в fingerprint. Takt не разбирает Markdown-планы в task AST.

`subworkflow` и `foreach` внутри `loop_group` используют ту же компиляцию в DAG.

## Именованные workflow профиля

```yaml
workflow: workflow.yaml
workflows:
  assist: workflows/assist.yaml
  piv-loop: workflows/piv-loop.yaml
```

`workflow` — default/роутер. Явный селектор `code:piv-loop` выбирает запись из `workflows`. Команды: `takt workflow list code`, `takt workflow describe code:piv-loop`.
