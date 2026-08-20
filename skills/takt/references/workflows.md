# Workflow Takt

## Каркас

```yaml
name: example
description: Пример процесса
provider: pi
model: main

nodes:
  - id: implement
    prompt: |
      Выполни запрос:
      $ARGUMENTS
```

## Общие поля узла

```yaml
- id: node-id
  depends_on: [previous]
  when: $previous.exit_code == 0
  trigger_rule: all_success
  provider: pi
  model: main
  context: fresh
  attempts:
    max: 3
  allow_failure: false
  timeout: 20m
```

`when` — только gate: `==`, `!=`, `&&`, `||` по `$<id>.*`/`$INPUTS.*`. Скобки, функции, арифметику и другие вычисления выносите в `script`/`command`/`prompt`, а `when` применяйте к их output. Правило языка: **YAML координирует, код вычисляет, агент принимает решения**.


`always_run: true` используй для cleanup/finally-узла после terminal-состояния всех зависимостей. Не сочетай его с `when` или другим `trigger_rule`: итоговая ошибка основного графа сохраняется.

`idle_timeout` доступен `command`/`prompt` и измеряет отсутствие нормализованных событий assistant. Общий `timeout` остаётся верхней границей попытки. Для `executor: external` автоматический expiry требует живого `takt daemon`.

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

Изоляция применяется ко всему workflow. У structural subworkflow policy активируется на его gate. Governed child Run по умолчанию применяет собственную policy либо режим `workflow.isolation`. Состояние и artifacts остаются в control workspace. Runtime автоматически удаляет только чистый успешный worktree. Ветка без коммитов поверх base удаляется; ветка с коммитами, грязный или неуспешный worktree сохраняются. Это не sandbox.


## Политики AI-узла

Только `command` и `prompt` поддерживают:

```yaml
- id: classify
  command: classify
  allowed_tools: []
  skills: []

- id: review
  command: review
  denied_tools: [edit, write]
  mcp: mcp/repository.json
  sandbox:
    filesystem: read_only
  requires: [tool_policy, mcp]
```

Явный пустой allowlist является запретом. Governed `workflow.policy` задаёт inherited upper bound ребёнка. Adapter capabilities проверяются до запуска; policy сохраняется в state и входит в fingerprint вместе с MCP/skill files. `sandbox` является assistant-enforced contract, а не OS isolation.

## Планирование

Независимые готовые `command`, `prompt` и `bash` без portable hooks и повторных попыток выполняются параллельной волной. Узлы с hooks или `attempts.max > 1` пока идут последовательным путём. Ошибка одной ветви не отменяет остальные уже запущенные ветви.

## Типы узлов

### Inline prompt

```yaml
- id: implement
  prompt: |
    Выполни запрос $ARGUMENTS.
    Замечания: $FEEDBACK
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
      signal: BUILD-CLEAN
      requires:
        - node: tests
          exit_code: 0
    until_bash: test -s "$validate.artifacts.report.path"
```

Также можно использовать компактный `loop` с scalar `until: BUILD-CLEAN` и
`fresh_context: false`. `until.signal` требует единственный валидный signal;
`until.requires` добавляет независимые evidence, а `until_bash` сохраняет
детерминированный stdout/stderr/exit evidence. Approval, `subworkflow`,
`foreach` и governed `workflow` разрешены внутри `loop_group`; `until.node`
использует публичный ID контейнера. Approval сохраняет активную итерацию и
после ответа продолжает её. Вложенный `loop_group` остаётся запрещён.

`max_iterations` должен быть в диапазоне `1..64`. Runtime сохраняет все завершённые итерации в `loop_iterations[]`; `loop_previous` остаётся совместимым snapshot последней итерации.

## Проверяемый JSON output

Шаблоны fail-closed: `$path` обязателен, `$path?` возвращает пустую строку при отсутствии, `$path:-default` задаёт fallback. `takt validate` заранее проверяет upstream output/artifact references, когда producer schema это позволяет.

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

Поддерживаются `object`, `array`, `string`, `boolean`, `number`, `integer`, `properties`, `required`, `enum`, `items`, `additionalProperties`. Нарушение контракта является `protocol`-ошибкой. Для retry укажи `attempts.retry_on: [protocol]`; точная ошибка попадёт в `$FEEDBACK`, а raw stdout останется в состоянии.

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
```

Действия hook: `continue`, `retry`, `fail`.
`hook.on_failure.session` accepts only `fresh` or `resume` and is valid only when
`action` is `retry`.

## Переменные

- `$ARGUMENTS` — вход пользователя;
- `$FEEDBACK` — вывод неуспешных hooks прошлой попытки;
- `$<id>.output` и `$<id>.output.<field>`;
- `$<id>.exit_code`;
- `$<id>.status`;
- `$LOOP_PREV.<id>.output`;
- `$MATRIX.item`, `$MATRIX.index`, `$MATRIX.total` и alias `$<as>` из `matrix.as`;
- `$<approval-id>.output` (с fallback к durable approval answer);
- `$ARTIFACTS_DIR` — каталог артефактов текущего Run.

Обязательная неизвестная ссылка является authoring/runtime ошибкой; для
осознанного отсутствия используй `$path?`, для fallback — `$path:-default`.
Legacy `${...}` и `$USER_MESSAGE` не поддерживаются. В shell hooks значения
node/artifact references quote-ятся как один аргумент, а `$ARTIFACTS_DIR`,
`$ARGUMENTS` и `$FEEDBACK` приходят через env.

## Статусы

`pending`, `running`, `waiting`, `completed`, `failed`, `errored`, `timed_out`, `cancelled`, `skipped`, `blocked`.

`allow_failure: true` действует только на штатный ненулевой exit code.

## Subworkflow

`subworkflow` подключает target Workflow и компилирует его в тот же DAG до запуска:

```yaml
- id: implement
  subworkflow:
    path: workflows/implement.yaml
    inputs:
      request: $ARGUMENTS
    output_node: result
```

В подключённом workflow доступны `$INPUTS.request` и другие явно переданные значения. Если terminal-узел один, `output_node` можно не указывать. При нескольких terminal-узлах поле обязательно.

Публичный ID контейнера сохраняется: следующий узел может использовать `depends_on: [implement]` и `$implement.output`. CLI скрывает namespaced узлы с `__`; полное состояние хранится на диске для resume. Approval принимается через публичный ID контейнера.

Контейнер поддерживает `id`, `depends_on`, `when`, `trigger_rule`, `provider`, `model` и `context`. Последние три поля задают defaults дочернего вызова. Положительный `attempts.max`, непустые timeout/hooks/native_hooks и `allow_failure: true` задаются внутри подключённого workflow.

Глубина композиции ограничена 16. Рекурсивные ссылки отклоняются при загрузке. Локальная команда ищется рядом с дочерним workflow и далее вверх до корня композиции.

## Governed child workflow

`workflow` запускает подключённое определение как отдельный Run:

```yaml
- id: implement
  workflow:
    path: workflows/implement.yaml
    input: $ARGUMENTS
    output_node: result
    isolation: inherit
```

Используй его, когда нужны отдельные state/events/artifacts/usage, самостоятельная worktree policy, каскадная отмена или наблюдаемая граница этапа. Пустой `isolation` применяет policy ребёнка; `inherit` использует execution workspace родителя; `worktree` создаёт отдельный managed worktree; `none` использует control workspace.

Родитель хранит `child_run_ids`, ребёнок — `parent_run_id` и `parent_node_id`. При approval родитель ждёт с `kind: child_run`; ответ можно передать через корневой Run. Команды:

```bash
takt children <run-id>
takt status <child-run-id>
takt cancel <run-id> --reason "stop"
```

Если у ребёнка один terminal-узел, `output_node` необязателен. При нескольких terminal-узлах он обязателен. Retry родительского узла создаёт новый child Run и сохраняет предыдущие попытки.

`subworkflow` выбирай для structural composition в одном Run; `workflow` — для отдельного lifecycle.

Для runtime-списка добавь `fan_out` внутрь `workflow`:

```yaml
- id: reviews
  depends_on: [classify]
  workflow:
    path: workflows/review.yaml
    input: $FANOUT.item
    isolation: inherit
    fan_out:
      items_from: $classify.output.reviewers
      as: reviewer
      max_parallel: 5
      join: all_success
```

Источник должен быть JSON-массивом в output upstream-узла. Доступны `$FANOUT.item`, `$FANOUT.index`, `$FANOUT.total` и алиас `as`. Каждый элемент получает отдельный child Run; resume сохраняет completed-элементы, output агрегируется в исходном порядке. `join` принимает `all_success`, `all_done`, `one_success`; `allow_empty: true` разрешает пустой список.

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
        name: $INPUTS.check
```

При `parallel: false` элементы выполняются строго по порядку; при `parallel: true` итерации становятся независимыми DAG-ветвями. Поддерживаются строки, числа, логические значения и inline JSON objects. Для объекта доступны `$INPUTS.check` как JSON и `$INPUTS.check.<field>` для полей. `$INPUTS.index` и `$INPUTS.check.index` содержат индекс с нуля.

`foreach` принимает ровно один источник: inline `items` или `items_from.path` к непустому YAML/JSON-массиву. Результат — JSON-массив outputs всех итераций в исходном порядке, независимо от порядка завершения параллельных ветвей. Содержимое внешнего файла входит в fingerprint. Takt не разбирает Markdown-планы в task AST.

`subworkflow` и `foreach` внутри `loop_group` используют ту же компиляцию в DAG.

## Matrix

Используй `matrix`, когда один Run должен последовательно выполнить
произвольный DAG для JSON items:

```yaml
- id: cases
  matrix:
    items_from: $INPUTS.cases
    as: case
    nodes:
      - id: candidate
        workflow:
          path: $MATRIX.item.workflow_path
          repository: $MATRIX.item.repository
          input: $MATRIX.item.input
          isolation: worktree
          keep_worktree: true
      - id: validate
        depends_on: [candidate]
        trigger_rule: all_done
        script:
          runtime: command
          path: tools/validate
          stdin: $MATRIX.item.request
    output_node: validate
```

`items_from` — одна ссылка на JSON array из root JSON `$INPUTS.*` или
structured node output. Ветки идут в исходном порядке, результат — ordered JSON
array. Максимум 1024 items; canonical duplicates отклоняются. Runtime проверяет
все dynamic child paths/repositories/fingerprints до branch 0. Resume не
повторяет completed branches и fail-closed отклоняет drift. Вложенные `matrix`
и `loop_group` запрещены, остальные обычные actions/composition разрешены.

## Script и типизированные артефакты

```yaml
- id: collect
  script:
    runtime: python
    path: tools/collect.py
    args: [--json]
    stdin: $INPUTS.request
    dependencies: [schemas/result.schema.json]
  output_format:
    type: object
    properties:
      entries: {type: array}
    required: [entries]
  output_type: collected-data
  output_mime: application/json
```

`runtime` принимает `command`, `python`, `node`, `go`, а также специальный
`validation`. У Python/Node можно заменить `path` на `inline`. `stdin` проходит
NonShell rendering и передаётся процессу byte-for-byte. `working_directory`
вычисляется относительно workflow. Script source и `dependencies` входят в
fingerprint и блокируют resume после изменения.

Для сохранения файла после успешного узла добавь `output_type`, MIME и `output_path`. Без `output_path` сохраняется Output узла. Используй `$collect.artifacts.collected-data.path` и другие metadata-поля (`sha256`, `mime`, `size`, producer IDs). Governed children и fan-out передают ссылки родителю. CLI: `takt artifacts <run-id> [--node ...] [--type ...] [--recursive]`.

## Assessment

`assessment` записывает immutable результат проверки другого terminal Run:

```yaml
- id: assess
  depends_on: [validate, evidence]
  assessment:
    role: primary
    target_run_id: $candidate.child_run_id
    result_from: $validate.output
    scope:
      case_id: $MATRIX.item.case_id
      repeat: $MATRIX.item.repeat
    evidence: [$evidence.artifacts.evaluation-evidence]
```

Primary требует deterministic `bash|script|adapter` result в формате
`takt-validation/v1alpha1`, `case_id`, положительный `repeat` и immutable
evidence. Assistant result используй только как `role: advisory`. `valid:false`
успешно сохраняется; malformed result, nonterminal target, invalid provenance,
missing/corrupt evidence или persistence error завершают Run ошибкой.
Assessment pin-ит `target.result_revision`, не изменяет target и становится
stale после нового terminal result target. Чтение:

```bash
takt run assessment <run-id> --role primary
takt run stats <run-id> --check-gates
takt run inspect <run-id> --case case-id --repeat 1
```

## Именованные workflow профиля

```yaml
workflow: workflow.yaml
workflows:
  assist: workflows/assist.yaml
  piv-loop: workflows/piv-loop.yaml
```

`workflow` — default/роутер. Явный селектор `code:piv-loop` выбирает запись из `workflows`. Команды: `takt workflow list code`, `takt workflow describe code:piv-loop`.

## Внешний executor AI-узла

Для передачи одного `command` или `prompt` внешнему coding agent укажи `executor: external`. Takt сохранит resolved prompt/model/session/policy и приостановит Run. Worker обязан использовать MCP-цикл `takt.node.pending → claim`, передавать обычные события через `node.event`, а tool calls — через `node.tool.request → decide → start → complete`; обычные `attempts`, hooks, `output_format` и `output_type` продолжают действовать после submission.

Для блокирующего решения добавь:

```yaml
executor: external
tool_approval:
  mode: required
  tools: [write, bash]
  message: "Разрешить выбранный инструмент?"
requires: [tool_control, agent_events_v2, tool_events]
```

Такой workflow запускается только с executor, который заявил pre-execution `tool_control`.

## Domain adapter node

Для SCM/tracker/CI используй нейтральный `adapter`, а не provider-specific shell/CLI в workflow:

```yaml
- id: publish
  adapter:
    name: scm
    operation: change.create
    input: |
      {"title":$prepare.output.title}
  side_effect:
    mode: reconcile
```

`adapter.input` после rendering должен быть JSON object. Для внешней мутации предпочитай `side_effect.mode: reconcile`: Takt проверит reconcile capability до вызова, сохранит idempotency key/receipt и не сделает blind retry после `unknown`.
