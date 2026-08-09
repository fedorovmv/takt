---
name: takt
description: Создаёт, устанавливает, изменяет, проверяет и запускает Takt workflows, configs, Markdown-команды и профили разных кодинг-агентов через нейтральный adapter contract. Используй, когда нужно настроить Takt, выбрать модель или assistant, собрать параллельный DAG, структурированный роутер, retry/feedback, hooks, approval, loop_group, subworkflow, foreach, governed workflow, script-узлы, типизированные артефакты, политики инструментов/skills/MCP/sandbox, диагностировать workflow либо подготовить готовый .takt-профиль, проверить authoring diagnostics или управлять Takt через локальный MCP/daemon или запускать Dynamic Takt из основной сессии кодинг-агента.
---

# Работа с Takt

Помогай пользователю получить работающий профиль Takt, а не только пример YAML. Используй локальную документацию и CLI как источник истины. Не выдумывай поля, шаблонные переменные и runtime-семантику.

## Обязательный порядок

1. Найди рабочую директорию и существующие файлы `.takt/`, workflow, config и Markdown-команды.
2. Если работа идёт в репозитории Takt, прочитай `AGENTS.md`, `docs/03-specification.md` и подходящий пример из `examples/`.
3. Выбери фактический coding assistant через `default_assistant`. Используй встроенные Pi/OpenCode adapters либо совместимый process adapter для Codex, Oh My Pi, Qwen CLI и других хостов. Не меняй assistant без причины.
4. Определи минимальную форму решения:
   - `prompt` — короткая инструкция прямо в узле;
   - `command` — длинный или переиспользуемый prompt в Markdown;
   - `bash` — короткая детерминированная shell-команда;
   - `script` — версионируемый command/Python/Node/Go-скрипт с fingerprint исходника и зависимостей;
   - `adapter` — нейтральная SCM/tracker/CI операция через именованный process/MCP adapter; provider-specific имя остаётся только в config;
   - multi-repo — используй `.takt/workspace.yaml` и repository-aware Dynamic Plan; каждый изменяемый repository получает отдельный governed child Run/worktree, а публикация идёт через нейтральный SCM adapter.
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


## Доменные адаптеры SCM, tracker и CI

Для внешней инженерной системы используй `adapter`-узел вместо GitHub/Jira-команд внутри workflow:

```yaml
- id: publish
  adapter:
    name: scm
    operation: change.create
    input: |
      {"title":"${nodes.prepare.output.title}"}
  side_effect:
    mode: reconcile
```

Конкретный process или MCP server задаётся в `config.yaml`. Перед authoring проверь `takt adapter doctor <name> --workspace .`: обязательная operation должна присутствовать в capability declaration, а операция с `side_effect.mode: reconcile` — также в reconcile capabilities. После `unknown` не создавай ручной повтор той же операции: runtime обязан сначала сверить внешний факт с тем же idempotency key. Для примера используй `examples/adapter-platform/`. Для reference implementations v0.1.49 смотри `examples/reference-adapters/`: `qwen-takt-adapter` имеет только `agent_events_v2/session_events/usage_events` и не должен использоваться для node policy, требующей tool/skill/MCP/sandbox enforcement; `takt-github-scm-adapter` реализует neutral SCM через `gh` и получает execution workspace от Takt. В multi-repo publication передавай точный `repository_workspace`, а не базовый checkout.

## Переносимые пакеты BlockPackage

Для повторно используемых project/corporate/global процессов предпочитай `takt package` ручному редактированию `profile.block_packages`:

```bash
takt package install ./package --scope project --workspace .
takt package install git+ssh://git.corp/takt/platform-code.git --scope corporate --ref v2.3.1 --workspace .
takt package doctor --workspace .
takt package sync --workspace .
```

Locked package автоматически подключается к каталогу профиля. При совпадении блоков действует `project > corporate > global > builtin`, но governance всех уровней объединяется fail-closed. Перед запуском учитывай `dependencies`, `requirements.takt` и adapter requirements: required capability должна пройти preflight, preferred может быть исключена Router/Planner. Для корпоративной поставки проверь `.takt/package-policy.yaml`; не обходи source allowlist, checksum или обязательную Ed25519 signature ручным копированием файлов.

## Локальный MCP и daemon

Для одноразового управления из coding-agent host запускай `takt mcp --surface agent --workspace .`; это безопасная поверхность из пяти `takt.task.*` операций. Когда Run должен пережить закрытие клиента или к одному workspace подключаются несколько локальных агентов, используй `takt daemon start --workspace .` и `takt mcp --daemon --surface agent --workspace .`. Наблюдай через `takt.task.status/explain`; операторский CLI может использовать `takt run summary`, `takt events --daemon --follow` и полную operator surface. Approval подтверждай только при наличии решения пользователя. Полный MCP-контракт: `references/mcp.md`.

## Строгий режим хоста кодинг-агента

Для host-managed выполнения используй `/takt` из `integrations/coding-agent-host-control/pi` или `integrations/coding-agent-host-control/opencode`. Go API поддерживает strict contract, но bundled Pi/OpenCode integrations заявляют только `guarded`: Pi не имеет подтверждённого completion gate, а OpenCode V2 не прошёл live smoke. Skill и обычный MCP-вызов также являются advisory/guarded и не должны называться строгим контролем.

Основная сессия показывает preview, подтверждает запуск, отправляет steering и читает статус/артефакты. Код и shell выполняют только worker-сессии текущих фаз Takt. После перезапуска host extension обязан восстановить managed session через `takt.host.find`.

## Автономные Run

Для длительных задач используй daemon и операционный API, а не ручной polling одного `run_id`:

```bash
takt runs --active --daemon
takt attention --daemon
takt run pause <run-id> --daemon
takt run resume <run-id> --daemon
takt run retry <run-id> --node <id> --daemon
takt run summary <run-id> --daemon
takt notify list --unread
```

Pause безопасная: новые узлы и fan-out batches не запускаются, текущая attempt заканчивает границу узла. После daemon restart Takt выполняет PID-based recovery как новую attempt; критичные внешние операции должны быть идемпотентными. Для кодинг-агента используй `/takt-runs`, `/takt-attention`, `/takt-pause`, `/takt-resume`, `/takt-result`.

## Simple Reliable Task Router

Для обычной многошаговой разработки предпочитай компактный task API:

```bash
takt task start "<задача>" --workspace .
takt task status <task-id> --workspace .
takt task explain <task-id> --workspace .
```

Router внутри Takt выбирает готовый workflow, стабильный `simple-reliable` или bounded Dynamic Plan. Ошибка семантического router не блокирует обычную задачу: используется `simple-reliable + inspect_first`. Дополнительные baseline, independent test design и enhanced review включаются по сигналам риска и не требуют отдельного пользовательского YAML. Все маршруты компилируются в обычный Workflow и исполняются одним scheduler.

Основная LLM через agent MCP видит только `takt.task.start|status|respond|stop|explain`. Host, worker и operator protocols подключаются отдельными surfaces и не должны попадать в её каталог tools.

## Dynamic Takt из кодинг-агента

Используй Dynamic Takt, когда задача требует отдельного плана, динамической инвентаризации, параллельных исполнителей или пересмотра оставшихся шагов. Основная сессия выбранного кодинг-агента остаётся пользовательским интерфейсом; Takt планирует и отслеживает Run, а отдельные worker-сессии выполняют фазы своими штатными инструментами.

Пользовательский путь:

```bash
takt plan "Проверь совместимость MCP-инструментов" --workspace .
takt execute <plan-id> --workspace . --confirm
# Для Run, который должен пережить закрытие основной сессии:
takt execute <plan-id> --workspace . --confirm --daemon
takt plan get <plan-id> --workspace .
takt steer <plan-id> "Сложные расхождения только задокументируй" --workspace .
takt plan promote <plan-id> --name audit-mcp-compatibility --workspace .
```

Через MCP используй высокоуровневые `takt.plan`, `takt.execute`, `takt.plan.get`, `takt.run.steer` и `takt.plan.promote`. Прямой stdio MCP выполняет `takt.execute` до terminal/waiting; MCP через daemon запускает его отсоединённо. Сначала покажи пользователю preview, бюджеты, требуемые фазы и необходимость подтверждения. `takt.execute` вызывай с `confirm: true` только после явного подтверждения, если политика проекта не разрешает автоматический запуск.

`WorkflowPlan` является ограниченным промежуточным представлением, а не вторым runtime. Он использует только блоки из явно подключённых `block_packages`, после чего компилируется в обычный Takt Workflow с governed child Runs. Встроенный пакет содержит `discover`, `investigate`, `baseline`, `test-design`, `implement`, `validate`, `review`, `adversarial-verify`, `synthesize`; корпоративный пакет может сузить политики, бюджеты и разрешённые интеграции либо добавить собственные блоки. Перепланирование допускается только в явных checkpoint и создаёт новую revision; выполненные фазы не переписываются.

`steer` сохраняет уточнение для ближайшей контрольной точки. При статусе `waiting` передай пользователю причину и используй steering как ответ. После успешного завершения предлагай `plan promote`: команда обобщает план в проектный workflow и повторно проверяет его до сохранения.

Для простого вопроса или небольшого локального изменения не вызывай Takt автоматически. Основной кодинг-агент выполняет такую задачу напрямую. Dynamic Takt нужен, когда ценность дают отдельное состояние, несколько исполнителей, параллельность, контрольный бюджет или долговременный Run.

## Доверенные пакеты блоков

Перед динамическим планированием проверь каталог:

```bash
takt block list --profile code --workspace .
takt block describe <name> --profile code --workspace .
takt block validate path/to/package.yaml
```

Профиль подключает пакет через `block_packages`. Не добавляй путь автоматически: каждый пакет является явной доверенной границей. Блок обязан иметь один итоговый узел со структурированным `output_format`; перечисленные `output_paths` должны существовать в этой схеме. Для `map` указывай точный объявленный путь типа `array`. Блок не должен запускать governed child Run — дочерние процессы создаются фазами `WorkflowPlan`, чтобы budgets оставались проверяемыми.

Корпоративный пакет может объявить required blocks/checks, branch rules, change-request template, allowed integrations, policy и верхние limits. Ограничения нескольких пакетов объединяются в более строгую сторону. После preview изменение package/workflow fingerprint требует нового плана.

Начиная с v0.1.39 пакет может объявлять внутренние `roles`. Не создавай отдельные глобальные agents в Pi/OpenCode/Codex/Qwen CLI ради этих ролей: Takt связывает block с RoleDefinition и перед worker-сессией компилирует bounded `TaskBrief`. Для проверок используй `checks` с `level: required|preferred` и `reaction: deny|repair|warn`. `repair` предназначен для автоматически исправимой технической ошибки, `deny` — для обязательной границы, `warn` — для замечания, которое не должно останавливать обычную задачу.

## Источники истины

При наличии репозитория Takt используй их в таком порядке:

1. `schemas/*.json` и `docs/03-specification.md` — внешний контракт;
2. `docs/09-runtime-semantics.md` — статусы, retry, hooks, loops и resume;
3. `docs/10-assistant-adapter-spec.md` — assistants, Pi и OpenCode;
4. `examples/` — рабочие композиции;
5. `takt validate ... --json` — окончательная проверка конкретного профиля.

В пользовательском проекте сначала изучай его `.takt/`, `AGENTS.md`, README, документацию инструментов и существующие примеры. Не заменяй предметные правила общими предположениями.

## Критичные правила

- Узел определяет ровно одно действие: `command`, `prompt`, `bash`, `script`, `adapter`, `approval`, `loop_group`, `subworkflow`, `foreach` или `workflow`.
- Приоритет assistant/model: узел → frontmatter Markdown-команды → `workflow.defaults`.
- Имена моделей в workflow ссылаются на aliases из `config.models`, а не напрямую на provider ID.
- `session: resume` требует реального сохранения Session ID; не подменяй неуспешный resume на fresh.
- OpenCode запускается через `opencode run --format json`; не парси TUI и не подменяй его собственный агентный цикл логикой Takt.
- `auto_approve: true` для OpenCode используй только в доверенной рабочей директории.
- Для исправления результата используй детерминированную проверку в hook и `on_failure.action: retry`.
- Для transient `attempts.retry_on` можно добавить bounded `backoff` (`initial`, `multiplier`, `max`, `jitter`); cancellation и неизвестный side effect не превращай в обычный retry.
- Секреты в process/script env передавай как `secret://ENV_NAME`, чтобы durable state/events/artifacts проходили через redaction; не вставляй literal secret в task input.
- `${feedback}` содержит вывод неуспешных hooks предыдущей попытки.
- Текст агента и наличие файла сами по себе не подтверждают успех; нужен bash-валидатор или другой детерминированный gate.
- Approval оформляй отдельным узлом. Внутри `loop_group` он сохраняет активную итерацию и после `takt answer` продолжает её.
- `loop_group.max_iterations` задавай в диапазоне `1..64`. Все завершённые итерации сохраняются в `loop_iterations[]`, а `loop_previous` остаётся snapshot последней итерации.
- Вложенные `loop_group` в `takt/v1alpha1` и целевом `v0.2` не поддерживаются. `subworkflow`, `foreach`, governed `workflow` и approval внутри `loop_group` разрешены.
- `allow_failure: true` разрешает только штатный ненулевой exit code, но не timeout, cancellation или ошибку запуска.
- Bash stdout/stderr сохраняются отдельно, а `${nodes.<id>.output}` содержит объединённый вывод. Script stdout/stderr также сохраняются раздельно; `output_format` меняет только нормализованный Output.
- Validation envelope `takt-validation/v1alpha1` выводится только в stdout; логи валидатора идут в stderr.
- Takt использует стандартный YAML parser `go.yaml.in/yaml/v3` и строгие публичные поля Takt. Для многострочного prompt или bash используй block scalar `|`.
- Markdown-план не преобразуй в task AST ради `foreach`: используй явный `foreach.items` или `foreach.items_from.path` к YAML/JSON-массиву.
- Неподдерживаемая capability должна завершать узел до вызова модели; не описывай ограничения только в prompt.
- Для `command/prompt` filesystem/network policy остаётся assistant-enforced. Для локального `bash/script` используй `sandbox.enforcement: required|optional`, когда нужен реальный OS wrapper (`bwrap` Linux / `sandbox-exec` macOS); `required` должен fail-closed при отсутствии backend.
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

Для классификации, маршрутизации и других машинных решений задавай `output_format`. `input.schema` и `output_format` используют один версионированный контракт `takt-schema-subset/v1`, а не произвольный JSON Schema. Поддерживаются `type`, `description`, `properties`, `required`, строковый `enum`, `items`, `minItems/maxItems/uniqueItems`, `minLength/maxLength/pattern`, `minimum/maximum`, `minProperties/maxProperties` и boolean `additionalProperties`. `$ref`, `$defs`, `oneOf/anyOf/allOf`, `const/default/format` и schema-valued `additionalProperties` не используй. Точную машиночитаемую границу проверяй через `takt compatibility schema`.

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

Перед переносом конфигурации на другую машину или обновлением внешнего исполнителя используй `takt compatibility check --config <path>`. Для release/CI-preflight добавляй `--strict`; `--live` проверяет версию Pi/OpenCode и `Describe()` domain adapters, но сам по себе не доказывает live host conformance и не переводит guarded-интеграцию в strict. Текущий support boundary смотри через `takt compatibility matrix`, а field-level решения будущего `v1beta1` — через `takt compatibility fields`.

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

Не заявляй, что профиль готов, пока `takt validate` не прошёл. Если запуск невозможен из-за отсутствия выбранного кодинг-агента, credentials, модели или предметного инструмента, явно отдели проверенную структуру от непроверенной внешней интеграции.

## Дополнительные материалы

Читай только нужные разделы:

- `references/configuration.md` — models, assistants и выбор исполнения;
- `references/workflows.md` — поля workflow, переменные, зависимости и статусы;
- `references/patterns.md` — готовые композиции;
- `references/troubleshooting.md` — диагностика типовых ошибок;
- `assets/validated-agent-profile/` — копируемый стартовый профиль.

## Evidence, baseline и external reconciliation

Начиная с v0.1.40 Dynamic Takt сохраняет внутренний `EvidenceManifest`: baseline, fingerprints известных failures, structured check evidence и verdict, привязанный к candidate content SHA-256. Не пытайся эмулировать это свободным текстом в skill или prompt. Trusted check должен возвращать структурированный результат, а Takt сам классифицирует неизменившееся baseline-падение и инвалидирует stale verdict после изменения candidate.

Для внешней мутации, результат которой нельзя безопасно повторить после потери worker, используй только `executor: external` с `side_effect.mode: reconcile`. После истёкшего claim внешний adapter обязан выполнить reconciliation и сообщить `not_applied`, `applied` с receipt/result или `unknown`; blind retry для такого узла запрещён.


## Structured Task Sources

Если задача приходит из issue/tracker/PRD, предпочитай настроенный `task_sources` и запуск `takt task start --source <name> --source-ref <ref>` вместо ручного копирования текста. Source adapter формирует normalized Task с immutable `source.revision`; тот же Task передаётся Router/Planner/Replanner. Не моделируй ingestion как `adapter`-узел workflow.

## Human-reviewed Learning Loop

Используй `takt learn` только для повторяемого durable pattern из истории Run. Сначала `takt learn scan --min-runs 2`, затем создай candidate через `learn propose`, зафиксируй решение человека через `learn review` и приложи versioned matrix report через `learn evaluate`. `learn stage` допустим только после passing regression gates.

Staged candidate находится в `.takt/learning/ready/<proposal-id>`. Не подключай его автоматически к package lock, profile `block_packages` или assistant skills: активация learned skill/block является отдельным явным действием после review/evaluation.
