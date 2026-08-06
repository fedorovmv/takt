---
name: takt
description: Создаёт, устанавливает, изменяет, проверяет и запускает Takt workflows, configs, Markdown-команды и профили кодовых агентов Pi/OpenCode. Используй, когда нужно настроить Takt, выбрать модель или assistant, собрать параллельный DAG, структурированный роутер, retry/feedback, hooks, approval, loop_group, subworkflow, foreach, governed workflow, script-узлы, типизированные артефакты, политики инструментов/skills/MCP/sandbox, диагностировать workflow либо подготовить готовый .takt-профиль, проверить authoring diagnostics или управлять Takt через локальный MCP/daemon или запускать Dynamic Takt из основной сессии кодинг-агента.
---

# Работа с Takt

Помогай пользователю получить работающий профиль Takt, а не только пример YAML. Используй локальную документацию и CLI как источник истины. Не выдумывай поля, шаблонные переменные и runtime-семантику.

## Обязательный порядок

1. Найди рабочую директорию и существующие файлы `.takt/`, workflow, config и Markdown-команды.
2. Если работа идёт в репозитории Takt, прочитай `AGENTS.md`, `docs/03-specification.md` и подходящий пример из `examples/`.
3. Выбери внешний coding assistant по существующей среде проекта: `pi` или `opencode`. Не меняй assistant без причины.
4. Определи минимальную форму решения:
   - `prompt` — короткая инструкция прямо в узле;
   - `command` — длинный или переиспользуемый prompt в Markdown;
   - `bash` — короткая детерминированная shell-команда;
   - `script` — версионируемый command/Python/Node/Go-скрипт с fingerprint исходника и зависимостей;
   - hook с `retry` — проверка и исправление результата;
   - `approval` — отдельное сохраняемое решение пользователя;
   - `loop_group` — только когда нужен повтор вложенного DAG, а обычных attempts недостаточно;
   - `subworkflow` — когда блок должен компилироваться в общий DAG и общий Run;
   - `workflow` — когда этапу нужен отдельный Run ID, state/events/artifacts/usage, cancellation или собственная worktree-политика;
   - `foreach` — для явно заданного inline-списка или внешнего YAML/JSON-массива, без скрытого разбора Markdown;
   - `output_type`/`output_mime`/`output_path` — для результата, который должен стать проверяемым артефактом и передаваться между Run.
   - `allowed_tools`/`denied_tools`, `skills`, `mcp`, `sandbox`, `requires` — для проверяемых ограничений AI-узла; явный `allowed_tools: []` означает отсутствие инструментов;
   - `always_run` — только для cleanup/finally после terminal dependencies;
   - `idle_timeout` — для AI-узла, который обязан регулярно публиковать activity events.
5. Сначала используй существующие model aliases и assistants из config. Новые добавляй только при необходимости.
6. Внеси минимальные изменения и проверь их командой `takt validate`; для CI используй `--warnings-as-errors`.
7. Если пользователь просит рабочий запуск и среда готова, выполни `takt run`; при `waiting` покажи запрос approval и продолжи через `takt answer` только после ответа пользователя.
8. В ответе перечисли изменённые файлы, фактически выбранные assistant/model и выполненные проверки.


## Готовые профили

Если пользователю нужен типовой процесс, сначала проверь наличие встроенного профиля. Для автономной разработки по Markdown-плану используй:

```bash
takt init code
takt validate code --workspace . --json
takt run code --workspace . --input docs/plan.md --json
```

Профиль хранится в `.takt/profiles/code/`. Markdown-файл остаётся авторитетным планом: Takt передаёт агенту его путь и содержимое, но не преобразует его в обязательный JSON/YAML список задач. Формализованные входные адаптеры должны оставаться расширением, а не условием работы профиля.

## Локальный MCP и daemon

Для одноразового управления из coding-agent host запускай `takt mcp --workspace .`. Когда Run должен пережить закрытие клиента или к одному workspace подключаются несколько локальных агентов, используй `takt daemon start --workspace .` и `takt mcp --daemon --workspace .`. Сохраняй `run_id`, наблюдай через `takt.run.get/events` либо `takt events --daemon --follow`. Approval подтверждай только при наличии решения пользователя. Полный MCP-контракт: `references/mcp.md`.

## Dynamic Takt из кодинг-агента

Используй Dynamic Takt, когда задача требует отдельного плана, динамической инвентаризации, параллельных исполнителей или пересмотра оставшихся шагов. Основная сессия Pi/OpenCode остаётся пользовательским интерфейсом; Takt планирует и отслеживает Run, а отдельные сессии coding assistant выполняют фазы своими инструментами.

Пользовательский путь:

```bash
takt plan "Проверь совместимость MCP-инструментов" --workspace .
takt execute <plan-id> --workspace . --confirm
takt plan get <plan-id> --workspace .
takt steer <plan-id> "Сложные расхождения только задокументируй" --workspace .
takt plan promote <plan-id> --name audit-mcp-compatibility --workspace .
```

Через MCP используй высокоуровневые `takt.plan`, `takt.execute`, `takt.plan.get`, `takt.run.steer` и `takt.plan.promote`. Сначала покажи пользователю preview, бюджеты, требуемые фазы и необходимость подтверждения. `takt.execute` вызывай с `confirm: true` только после явного подтверждения, если политика проекта не разрешает автоматический запуск.

`WorkflowPlan` является ограниченным промежуточным представлением, а не вторым runtime. Он использует только разрешённые блоки `discover`, `investigate`, `implement`, `validate`, `review`, `adversarial-verify`, `synthesize`, после чего компилируется в обычный Takt Workflow с governed child Runs. Перепланирование допускается только в явных checkpoint и создаёт новую revision; выполненные фазы не переписываются.

`steer` сохраняет уточнение для ближайшей контрольной точки. При статусе `waiting` передай пользователю причину и используй steering как ответ. После успешного завершения предлагай `plan promote`: команда обобщает план в проектный workflow и повторно проверяет его до сохранения.

Для простого вопроса или небольшого локального изменения не вызывай Takt автоматически. Основной кодинг-агент выполняет такую задачу напрямую. Dynamic Takt нужен, когда ценность дают отдельное состояние, несколько исполнителей, параллельность, контрольный бюджет или долговременный Run.

## Источники истины

При наличии репозитория Takt используй их в таком порядке:

1. `schemas/*.json` и `docs/03-specification.md` — внешний контракт;
2. `docs/09-runtime-semantics.md` — статусы, retry, hooks, loops и resume;
3. `docs/10-assistant-adapter-spec.md` — assistants, Pi и OpenCode;
4. `examples/` — рабочие композиции;
5. `takt validate ... --json` — окончательная проверка конкретного профиля.

В пользовательском проекте сначала изучай его `.takt/`, `AGENTS.md`, README, документацию инструментов и существующие примеры. Не заменяй предметные правила общими предположениями.

## Критичные правила

- Узел определяет ровно одно действие: `command`, `prompt`, `bash`, `script`, `approval`, `loop_group`, `subworkflow`, `foreach` или `workflow`.
- Приоритет assistant/model: узел → frontmatter Markdown-команды → `workflow.defaults`.
- Имена моделей в workflow ссылаются на aliases из `config.models`, а не напрямую на provider ID.
- `session: resume` требует реального сохранения Session ID; не подменяй неуспешный resume на fresh.
- OpenCode запускается через `opencode run --format json`; не парси TUI и не подменяй его собственный агентный цикл логикой Takt.
- `auto_approve: true` для OpenCode используй только в доверенной рабочей директории.
- Для исправления результата используй детерминированную проверку в hook и `on_failure.action: retry`.
- `${feedback}` содержит вывод неуспешных hooks предыдущей попытки.
- Текст агента и наличие файла сами по себе не подтверждают успех; нужен bash-валидатор или другой детерминированный gate.
- Approval оформляй отдельным узлом. Внутри `loop_group` он сохраняет активную итерацию и после `takt answer` продолжает её.
- Вложенные `loop_group` в `takt/v1alpha1` не поддерживаются. `subworkflow`, `foreach`, governed `workflow` и approval внутри `loop_group` разрешены.
- `allow_failure: true` разрешает только штатный ненулевой exit code, но не timeout, cancellation или ошибку запуска.
- Bash stdout/stderr сохраняются отдельно, а `${nodes.<id>.output}` содержит объединённый вывод. Script stdout/stderr также сохраняются раздельно; `output_format` меняет только нормализованный Output.
- Validation envelope `takt-validation/v1alpha1` выводится только в stdout; логи валидатора идут в stderr.
- Takt поддерживает ограниченный YAML subset. Для многострочного prompt или bash используй block scalar `|`.
- Markdown-план не преобразуй в task AST ради `foreach`: используй явный `foreach.items` или `foreach.items_from.path` к YAML/JSON-массиву.
- Неподдерживаемая capability должна завершать узел до вызова модели; не описывай ограничения только в prompt.
- Filesystem/network policy текущей версии является assistant-enforced и не заменяет OS sandbox.
- Значимые файлы публикуй через `output_type` и `output_path`; downstream использует `${nodes.<id>.artifacts.<type>.path}`, а не временный путь producer.
- Обязательная шаблонная ссылка записывается `${path}` и должна разрешиться; отсутствие допускай только явно через `${path?}` или `${path:-default}`.
- `takt validate` проверяет output/artifact references и adapter capabilities до Run; не откладывай эти ошибки до модели.
- Не добавляй `system_prompt`, `user_prompt`, автоматический model fallback или иные поля, которых нет в текущем контракте.

## Переиспользование workflow

Используй `subworkflow`, когда несколько профилей или фаз должны выполнять один и тот же DAG:

```yaml
- id: review
  subworkflow:
    path: workflows/review.yaml
    inputs:
      plan: ${input}
    output_node: result
```

В подключённом workflow вход читается как `${inputs.plan}`. Если terminal-узел один, `output_node` можно не задавать. При нескольких terminal-узлах он обязателен.

`foreach` принимает inline-список или внешний YAML/JSON-массив; для независимых элементов включай `parallel: true`:

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

Публичный узел `checks` завершается после всех итераций и возвращает JSON-массив outputs в порядке элементов, даже если параллельные ветви завершились иначе. Изменение внешнего списка меняет fingerprint.

На контейнере можно задать `assistant`, `model` и `session` как defaults дочернего вызова. `attempts`, `timeout`, hooks, `native_hooks` и `allow_failure` задавай внутри дочернего workflow. Глубина композиции ограничена 16; рекурсивные ссылки отклоняются.

Используй governed `workflow`, когда дочерний процесс должен быть отдельной управляемой единицей:

```yaml
- id: implementation
  workflow:
    path: workflows/feature-development.yaml
    input: ${input}
    output_node: summary
    isolation: inherit
```

Ребёнок получает отдельные Run ID, state, events, artifacts и usage. `isolation` принимает `inherit`, `worktree`, `none` или пустое значение для собственной policy ребёнка. `takt children` показывает детей, `takt cancel` каскадирует отмену, а `takt answer` по корневому Run проходит к approval ребёнка. Retry узла создаёт новый child Run. Для динамического массива из upstream JSON используй `workflow.fan_out`: один governed child Run на элемент, `max_parallel`, ordered aggregation и устойчивый resume.

## Script-узлы и артефакты

Используй `script`, когда детерминированная логика длиннее простой shell-команды, должна тестироваться отдельно или имеет явные зависимости:

```yaml
- id: prepare
  script:
    runtime: command
    path: tools/prepare
    dependencies: [schemas/input.schema.json]
  output_format:
    type: object
    properties:
      files:
        type: array
        items: {type: string}
    required: [files]
  output_type: prepared-input
  output_mime: application/json
```

Runtime: `command`, `python`, `node`, `go`, а также специальный `validation` для последовательного исполнения `input.validation_commands`. Для Python/Node допустим `inline`; для command/Go нужен исполняемый `path`. Исходник и `dependencies` входят в fingerprint. Takt не устанавливает зависимости runtime автоматически.

Для файла, созданного AI-узлом или script, укажи:

```yaml
output_type: plan
output_mime: text/markdown
output_path: $ARTIFACTS_DIR/plan.md
```

Проверь результат через `takt artifacts <run-id> --recursive`. Ссылки child Run поднимаются родителю, но producer metadata сохраняет фактический Run и Node.

## Структурированный вывод и умный роутер

Для классификации, маршрутизации и других машинных решений задавай `output_format`. Он поддерживает проверяемый JSON-Schema-подобный subset: базовые типы, `properties`, `required`, `enum`, `items`, `additionalProperties`, min/max для массивов, строк, чисел и объектов, а также `pattern`.

```yaml
- id: route
  command: route-workflow
  output_format:
    type: object
    properties:
      workflow:
        type: string
        enum: [assist, fix-github-issue, smart-pr-review]
      reason:
        type: string
    required: [workflow, reason]
    additionalProperties: false

- id: fix
  depends_on: [route]
  when: nodes.route.output.workflow == "fix-github-issue"
  workflow:
    path: workflows/fix-github-issue.yaml
    input: ${input}
```

Runtime принимает ровно одно JSON-значение и завершает узел `protocol`-ошибкой при нарушении схемы. В шаблонах и `when` доступны вложенные пути `${nodes.route.output.workflow}` и `nodes.route.output.workflow`.

Профиль может объявить именованный каталог `workflows`. Запускай роутер через `takt run code`, конкретный процесс — через `takt run code:piv-loop`, список — через `takt workflow list code`.

## Выбор prompt или command

Используй inline `prompt`, когда инструкция короткая и относится только к одному workflow:

```yaml
- id: implement
  assistant: pi
  model: main
  prompt: |
    Выполни запрос:
    ${input}

    Исправь замечания проверки:
    ${feedback}
```

Используй `command`, когда prompt длинный, повторяется или должен версионироваться отдельно:

```yaml
- id: implement
  command: implement
  model: main
```

```markdown
---
description: Выполняет задачу и исправляет замечания
assistant: pi
model: main
---

Выполни запрос:
${input}

Замечания проверки:
${feedback}
```

## Выбор Pi или OpenCode

Используй уже установленный assistant. Для OpenCode конфигурация выглядит так:

```yaml
assistants:
  opencode:
    type: opencode
    binary: opencode
    agent: build
    auto_approve: false
```

В workflow меняется только ссылка:

```yaml
defaults:
  assistant: opencode
  model: main
  session: resume
```

Модель передаётся OpenCode как `provider/id`, параметр `variant` — как вариант модели.

## Проверка результата

Минимальная проверка:

```bash
takt validate .takt/workflows/main.yaml \
  --config .takt/config.yaml \
  --workspace . \
  --json
```

Если бинарник собирается из исходников Takt:

```bash
go run ./cmd/takt validate <workflow> --config <config> --workspace <workspace> --json
```

Для запуска:

```bash
takt run .takt/workflows/main.yaml \
  --config .takt/config.yaml \
  --workspace . \
  --input request.md \
  --json
```

Не заявляй, что профиль готов, пока `takt validate` не прошёл. Если запуск невозможен из-за отсутствия Pi/OpenCode, credentials, модели или предметного инструмента, явно отдели проверенную структуру от непроверенной внешней интеграции.

## Дополнительные материалы

Читай только нужные разделы:

- `references/configuration.md` — models, assistants и выбор исполнения;
- `references/workflows.md` — поля workflow, переменные, зависимости и статусы;
- `references/patterns.md` — готовые композиции;
- `references/troubleshooting.md` — диагностика типовых ошибок;
- `assets/validated-agent-profile/` — копируемый стартовый профиль.
