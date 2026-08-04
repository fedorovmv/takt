---
name: takt
description: Создаёт, устанавливает, изменяет, проверяет и запускает Takt workflows, configs, Markdown-команды и профили кодовых агентов Pi/OpenCode. Используй, когда нужно настроить Takt, выбрать модель или assistant для узлов, собрать DAG, retry/feedback, hooks, approval, loop_group, subworkflow, foreach, диагностировать workflow либо подготовить готовый .takt-профиль.
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
   - `bash` — детерминированная команда;
   - hook с `retry` — проверка и исправление результата;
   - `approval` — отдельное сохраняемое решение пользователя;
   - `loop_group` — только когда нужен повтор вложенного DAG, а обычных attempts недостаточно;
   - `subworkflow` — когда блок процесса должен переиспользоваться как отдельный Workflow;
   - `foreach` — для явно заданного inline-списка или внешнего YAML/JSON-массива, без скрытого разбора Markdown.
5. Сначала используй существующие model aliases и assistants из config. Новые добавляй только при необходимости.
6. Внеси минимальные изменения и проверь их командой `takt validate`.
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

## Источники истины

При наличии репозитория Takt используй их в таком порядке:

1. `schemas/*.json` и `docs/03-specification.md` — внешний контракт;
2. `docs/09-runtime-semantics.md` — статусы, retry, hooks, loops и resume;
3. `docs/10-assistant-adapter-spec.md` — assistants, Pi и OpenCode;
4. `examples/` — рабочие композиции;
5. `takt validate ... --json` — окончательная проверка конкретного профиля.

В пользовательском проекте сначала изучай его `.takt/`, `AGENTS.md`, README, документацию инструментов и существующие примеры. Не заменяй предметные правила общими предположениями.

## Критичные правила

- Узел определяет ровно одно действие: `command`, `prompt`, `bash`, `approval`, `loop_group`, `subworkflow` или `foreach`.
- Приоритет assistant/model: узел → frontmatter Markdown-команды → `workflow.defaults`.
- Имена моделей в workflow ссылаются на aliases из `config.models`, а не напрямую на provider ID.
- `session: resume` требует реального сохранения Session ID; не подменяй неуспешный resume на fresh.
- OpenCode запускается через `opencode run --format json`; не парси TUI и не подменяй его собственный агентный цикл логикой Takt.
- `auto_approve: true` для OpenCode используй только в доверенной рабочей директории.
- Для исправления результата используй детерминированную проверку в hook и `on_failure.action: retry`.
- `${feedback}` содержит вывод неуспешных hooks предыдущей попытки.
- Текст агента и наличие файла сами по себе не подтверждают успех; нужен bash-валидатор или другой детерминированный gate.
- Approval оформляй отдельным узлом. Approval внутри `loop_group` не поддерживается.
- Вложенные `loop_group` в `takt/v1alpha1` не поддерживаются. `subworkflow` и `foreach` внутри `loop_group` разрешены; approval внутри цикла остаётся отдельным ограничением.
- `allow_failure: true` разрешает только штатный ненулевой exit code, но не timeout, cancellation или ошибку запуска.
- Bash stdout/stderr сохраняются отдельно, а `${nodes.<id>.output}` содержит объединённый вывод.
- Validation envelope `takt-validation/v1alpha1` выводится только в stdout; логи валидатора идут в stderr.
- Takt поддерживает ограниченный YAML subset. Для многострочного prompt или bash используй block scalar `|`.
- Markdown-план не преобразуй в task AST ради `foreach`: используй явный `foreach.items` или `foreach.items_from.path` к YAML/JSON-массиву.
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

Последовательный `foreach` принимает inline-список или внешний YAML/JSON-массив:

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

Публичный узел `checks` завершается после последней итерации и возвращает JSON-массив outputs в порядке элементов. Изменение внешнего списка меняет fingerprint. Параллельный режим пока отсутствует.

На контейнере можно задать `assistant`, `model` и `session` как defaults дочернего вызова. `attempts`, `timeout`, hooks, `native_hooks` и `allow_failure` задавай внутри дочернего workflow. Глубина композиции ограничена 16; рекурсивные ссылки отклоняются.

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
