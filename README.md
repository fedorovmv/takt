# Takt

Универсальный Go-runtime для воспроизводимых процессов с кодовыми агентами, одиночными вызовами моделей, детерминированными командами, циклами проверки и участием человека.

Проект вдохновлён моделью Archon, но не является портом его исходного кода. Takt реализует компактное подмножество наиболее полезных механизмов в Go и сохраняет строгую границу с Pi, OpenCode, Codex и другими кодовыми агентами.

## Область применения текущей версии

`v0.1.34-alpha` предназначена для **локального однопользовательского trusted runtime**. Workflow, config, Markdown-команды и рабочая директория считаются доверенными.

Локальный `takt daemon` поддерживает фоновые Run и несколько клиентов одного пользователя через Unix socket. Сетевой и многопользовательский запуск, а также выполнение конфигураций от недоверенных пользователей требуют sandbox, политики путей, изоляции сети, управления секретами и distributed locking. Эти режимы не поддерживаются.

## Что уже работает

- конфигурация моделей и исполнителей;
- Markdown-команды с frontmatter;
- workflow в YAML или JSON;
- DAG с параллельным выполнением независимых узлов, `depends_on`, `when`, `trigger_rule` и cleanup-семантикой `always_run`;
- единая семантика корневого DAG и дочернего DAG `loop_group`;
- узлы `command`, `prompt`, `bash`, `script`, `approval`, `loop_group`, `subworkflow`, `foreach`, `workflow`;
- reusable `subworkflow` компилируется в тот же DAG, а `workflow` запускает отдельный governed child Run;
- последовательный и параллельный `foreach` для inline-списков и внешних YAML/JSON-массивов без преобразования Markdown в task AST;
- `subworkflow` и `foreach` внутри `loop_group`;
- JSON-массив результатов всех итераций `foreach`;
- публичное состояние Run без внутренних развёрнутых ID;
- вложенные `loop_group` явно запрещены в `v1alpha1`;
- повтор узла после внешней проверки;
- переносимые hooks `before_node`, `after_node`, `before_complete`, `on_failure`;
- разделение ненулевого exit code, ошибки запуска, timeout и cancellation;
- `allow_failure`, разрешающий только ненулевой exit code;
- `all_done` после неуспешной зависимости;
- timeout всей попытки узла, включая portable hooks, и activity-based `idle_timeout` для AI-узлов;
- timeout/cancellation родительского `loop_group` сохраняют `timed_out`/`cancelled`;
- общий thread-safe лимит stdout/stderr process assistant;
- approval с сохранением состояния и продолжением через `takt answer`, включая повторные решения внутри `loop_group`;
- явное продолжение через `takt resume`;
- fingerprints workflow, config и Markdown-команд;
- блокировка Run при `answer` и `resume`;
- ревизии состояния и событий с проверкой согласованности;
- JSONL-журнал событий и файловые артефакты;
- адаптеры `mock`, универсальный `process` и специализированный `pi`;
- JSON-протокол `takt-assistant/v1alpha1` и потоковый `takt-assistant/v1alpha2` для внешних process assistants;
- fake-assistant contract suite: success, exit, start, timeout, cancel, concurrent output, malformed/strict protocol cases, fresh и resume;
- Pi RPC adapter и fake-Pi contract suite, включая model/thinking mapping, fresh/resume, ожидание `agent_settled`, автоматический retry, per-attempt usage delta, timeout/cancel, output limit и границу extension UI;
- OpenCode CLI adapter через `opencode run --format json`, с model/agent/variant mapping, проверенным resume, per-step usage, сохранением provider diagnostics при timeout/cancellation и contract suite;
- полное совпадение OS exit code и envelope `exit_code`, включая ноль;
- единый JSON envelope CLI для успеха и ошибок;
- строгий YAML subset с сохранением пустых строк в block scalar, path-aware `did you mean` и authoring diagnostics;
- расширенный проверяемый `output_format`, статическая диагностика output/artifact references и строгие `${path}`, `${path?}`, `${path:-default}`;
- именованные workflow профиля, `workflow list/describe` и селектор `profile:name`;
- профиль `code` 0.10.0 с 19 процессами разработки, умным роутером и шестью глубокими workflow со строгими JSON-входами, checkpoint artifacts, domain errors, Git/recovery semantics;
- управляемые Git worktree: политика workflow, отдельная ветка, безопасное удержание/очистка и `takt worktree list/remove/prune`;
- parent/child lifecycle с отдельными state/events/artifacts/usage, `takt children`, каскадным `takt cancel` и approval через корневой Run;
- динамический fan-out governed child Runs из структурированного output: устойчивые child ID, `max_parallel`, resume, ordered aggregation и join policies;
- script runtime `command|python|node|go|validation` с fingerprints исходника и зависимостей;
- типизированные артефакты с MIME, SHA-256, producer metadata, CLI `takt artifacts` и передачей parent/child/fan-out;
- локальный stdio MCP control plane с dual-era `initialize`/`server/discover`, 27 инструментами управления workflow/Run, внешними AI-узлами и отдельными tool calls, detached start, indexed revision events и bounded artifact content;
- Dynamic Takt: решение `existing|planned`, ограниченный `WorkflowPlan`, каталог разрешённых блоков, компиляция в обычные governed child Run, preview/confirmation, бюджеты, checkpoint-replanning, steering, plan revisions и продвижение completed-плана в workflow проекта;
- локальный `takt daemon` на Unix socket и файловом Store: background Runs, event subscriptions, MCP proxy, idle enforcement внешних workers и несколько клиентов без БД;
- event protocol v2: session lifecycle, tool request/allow/deny/start/complete, отдельная отмена tool call, artifact declaration с `call_id`, usage/diagnostic/terminal events и capability declaration;
- aggregate usage по узлам и отдельные execution records по каждой фактической попытке;
- `takt eval run/report` для воспроизводимой оценки каталогов заданий с fingerprints стратегии, benchmark, workspace и валидатора, версией assistant, requested/resolved model и предметными метриками качества;
- атрибуция tokens/cost по execution identity; смена assistant, его версии или resolved model между retry помечается как mixed;
- измеренные нулевые показатели сохраняются как `0`, а недоступные средние значения — как `null`;
- validation envelope сохраняется при любом terminal status quality-node; успех требует `completed && valid=true`;
- строгий контракт результата валидатора `takt-validation/v1alpha1`;
- только стандартная библиотека Go.

## Быстрый старт

```bash
make check

./bin/takt validate examples/route-dsl/workflow.yaml \
  --config examples/route-dsl/config.yaml

./bin/takt run examples/route-dsl/workflow.yaml \
  --config examples/route-dsl/config.yaml \
  --workspace examples/route-dsl \
  --input examples/route-dsl/specification.md
```

Демонстрационный процесс остановится на approval-узле и вернёт `run_id`. Продолжение:

```bash
./bin/takt answer <run-id> approve-result \
  --workspace examples/route-dsl \
  --value "Подтверждаю"
```

Повторное продолжение Run после временной ошибки CLI:

```bash
./bin/takt resume <run-id> --workspace examples/route-dsl
```

Прогон набора Route DSL заданий:

```bash
./bin/takt eval run examples/route-dsl-e2e/workflow.yaml \
  --config examples/route-dsl-e2e/config.yaml \
  --cases examples/route-dsl-eval/cases \
  --workspace-template examples/route-dsl-e2e \
  --output .takt/evals/qwen-resume \
  --answer approved \
  --strategy-id qwen-route-feedback-v1 \
  --benchmark-id route-dsl-real-10-v1 \
  --quality-node full-validation \
  --generation-node implement \
  --validator-id route-tool \
  --validator-version 1.0 \
  --validator-path route-tool \
  --repeat 3 \
  --replace \
  --json
```




## Локальное управление через MCP и daemon

Одноразовый stdio MCP:

```bash
takt mcp --workspace . --config .takt/config.yaml
```

Фоновый локальный процесс:

```bash
takt daemon start --workspace .
takt run workflow.yaml --workspace . --daemon --json
takt events <run-id> --workspace . --daemon --follow
takt mcp --workspace . --daemon
takt daemon stop --workspace .
```

Сервер работает по stdio и публикует `takt.workflow.list/describe`, `takt.run.start/get/resume/answer/cancel/children/artifacts/events`, `takt.node.pending/claim/event/complete/fail` и управляемый lifecycle `takt.node.tool.request/decide/start/complete/get/cancel` вместе с `takt.node.artifact.declare`. `run.start` по умолчанию возвращает durable `run_id` после принятия запуска; состояние и поток событий читаются отдельными вызовами по revision cursor. Поддерживаются legacy initialization до `2025-11-25` и stateless discovery `2026-07-28`.

Прямой MCP и daemon используют тот же файловый store, locks, fingerprints, governed children и worktree lifecycle, что CLI. Daemon слушает только Unix socket текущего пользователя, переживает закрытие клиента, но не является сетевым или многопользовательским сервером. Подробности: [Локальный MCP control plane v0.1.30](docs/44-local-mcp-control-plane-v0.1.30.md), [внешний executor v0.1.31](docs/45-agent-events-external-executor-v0.1.31.md) и [управляемые события и глубокие workflow v0.1.32](docs/46-controlled-agent-events-deep-workflows-v0.1.32.md), а также [authoring/daemon v0.1.33](docs/47-authoring-local-daemon-v0.1.33.md).

## Dynamic Takt из основной сессии кодинг-агента

```bash
takt plan "Проверь совместимость MCP-инструментов" --workspace .
takt execute <plan-id> --workspace . --confirm
takt plan get <plan-id> --workspace .
takt steer <plan-id> "Сложные расхождения вынеси в отчёт" --workspace .
takt plan promote <plan-id> --name audit-mcp-compatibility --workspace .
```

`takt plan` выбирает готовый процесс либо создаёт ограниченный task-specific `WorkflowPlan` из разрешённых блоков. План компилируется в обычный Takt Workflow: отдельные Pi/OpenCode worker-сессии выполняют governed child Run, а основная сессия кодинг-агента показывает preview, наблюдает события и передаёт approval/steering. Перепланирование происходит только в явных checkpoint и создаёт новую revision незавершённой части. MCP-инструменты: `takt.plan`, `takt.plan.get`, `takt.execute`, `takt.run.steer`, `takt.plan.promote`. Подробности: [Dynamic Takt v0.1.34](docs/48-dynamic-takt-v0.1.34.md).

## Профиль code: 19 процессов, умный роутер и глубокие workflow

```bash
takt init code
takt workflow list code
takt workflow describe code:piv-loop
takt run code --input "Исправь issue #123 и создай PR"
takt run code:comprehensive-pr-review --input "Проверь текущий PR"
```

Запуск `code` без суффикса выполняет schema-validated router node в корневом Run и запускает выбранный процесс как отдельный governed child Run. Каталог включает assist, GitHub issue/PR процессы, PIV, PRD, Ralph, архитектурный анализ, безопасный рефакторинг, adversarial development, Remotion и разрешение конфликтов. Шесть основных процессов принимают строгий JSON input и требуют типизированные checkpoints, evidence, Git preparation, validation/recovery и предметные error codes. Подробности: [Каталог процессов v0.1.24](docs/38-archon-workflow-catalog-v0.1.24.md) и [углубление процессов v0.1.32](docs/46-controlled-agent-events-deep-workflows-v0.1.32.md).

## Композиция workflow

```yaml
nodes:
  - id: implementation
    subworkflow:
      path: workflows/implementation.yaml
      inputs:
        plan: ${input}

  - id: checks
    depends_on: [implementation]
    foreach:
      as: check
      items: [lint, test]
      subworkflow:
        path: workflows/check.yaml
        inputs:
          name: ${check}
```

`subworkflow` и `foreach` разворачиваются до запуска в обычный DAG, включая дочерний DAG `loop_group`. Публичные ID `implementation` и `checks` остаются доступными для зависимостей и шаблонов, а внутренние ID скрыты из CLI-состояния. `foreach` принимает inline `items` или `items_from.path`, поддерживает `parallel: true` и возвращает JSON-массив результатов в порядке элементов; Markdown-планы Takt не преобразует во внутренний список задач.

Рабочий пример: [`examples/composition/`](examples/composition/). Изменяющие процессы профиля `code` запускаются в управляемом Git worktree; состояние и артефакты остаются в исходном checkout.

Для отдельного жизненного цикла используется `workflow`:

```yaml
- id: feature
  workflow:
    path: workflows/feature-development.yaml
    input: ${input}
    output_node: summary
```

Ребёнок получает собственный Run ID, state/events/artifacts и usage. Управление деревом:

```bash
takt children <run-id>
takt status <child-run-id>
takt cancel <run-id> --reason "остановлено пользователем"
```

Approval внутри ребёнка можно подтвердить через ID корневого Run и публичный ID `workflow`-узла. Подробности: [Governed child Runs v0.1.26](docs/40-governed-child-runs-v0.1.26.md).

Динамический fan-out управляемых детей задаётся внутри `workflow`:

```yaml
- id: reviews
  depends_on: [classify]
  workflow:
    path: workflows/review.yaml
    input: "Perspective: ${reviewer}"
    isolation: inherit
    fan_out:
      items_from: nodes.classify.output.reviewers
      as: reviewer
      max_parallel: 5
      join: all_success
```

Каждый элемент получает отдельный Run ID. Завершённые дети переиспользуются при resume, результат агрегируется в исходном порядке, а изменение массива блокирует небезопасное продолжение. Подробности: [Динамический fan-out v0.1.28](docs/42-governed-child-fanout-v0.1.28.md).

## Script-узлы и типизированные артефакты

```yaml
- id: build-index
  script:
    runtime: command
    path: tools/build-index
    dependencies: [schemas/index.schema.json]
  output_format:
    type: object
    properties:
      files:
        type: array
        items: {type: string}
    required: [files]
  output_type: plan-index
  output_mime: application/json
```

Поддерживаются `command`, `python`, `node` и `go`, file/inline source, args, env и working directory. Исходник и dependencies входят в fingerprint. `output_type` сохраняет Output либо `output_path` как снимок с checksum и producer metadata. Downstream-узлы используют `${nodes.build-index.artifacts.plan-index.path}`.

```bash
takt artifacts <run-id>
takt artifacts <run-id> --type plan --recursive
```

Артефакты governed children поднимаются родителю без потери producer Run. Подробности: [Script-узлы и типизированные артефакты v0.1.29](docs/43-script-nodes-typed-artifacts-v0.1.29.md).

## Скилл для настройки Takt

Каталог [`skills/takt/`](skills/takt/) содержит переносимый скилл для кодовых агентов. Он помогает:

- собирать `.takt/config.yaml`, workflow и Markdown-команды;
- выбирать assistant и model на уровне defaults, команды или узла;
- проектировать retry/feedback, hooks, approval, `loop_group`, `subworkflow`, `foreach` и governed `workflow`;
- использовать inline `prompt` и внешние команды;
- проверять профиль через `takt validate` и диагностировать ошибки;
- начинать с проверенного шаблона `skills/takt/assets/validated-agent-profile/`.

Основной файл скилла: [`skills/takt/SKILL.md`](skills/takt/SKILL.md).

## С чего продолжать разработку

Семантика runtime, process-протокол и специализированный Pi RPC adapter стабилизированы контрактными тестами. Воспроизводимый Route DSL end-to-end добавлен в `examples/route-dsl-e2e` и проверяется в `make check`.

Пакеты профилей, reusable `subworkflow`, параллельный DAG и оба режима `foreach` реализованы. Профиль `code` 0.10.0 содержит 19 процессов разработки и умный роутер с отдельным child Run для выбранного процесса. Интерактивные PIV/PRD-циклы возобновляют активную итерацию после approval, а структурированные классификаторы проверяются через `output_format`. Per-node политики инструментов, skills, MCP и assistant-enforced sandbox реализованы с проверкой возможностей adapter до запуска. Динамический fan-out дочерних Run реализован и используется smart/comprehensive review. Script-узлы и типизированные артефакты используются для review perspectives, планов и PRD. Локальная интеграция Takt через MCP реализована в v0.1.30-alpha; v0.1.31-alpha добавляет durable `executor: external`, v0.1.32-alpha завершает управляемый tool lifecycle и углубляет шесть основных workflow, v0.1.33-alpha добавляет строгий authoring preflight и локальный daemon, а v0.1.34-alpha — Dynamic Takt и coding-agent flow. Web UI, БД и удалённый многопользовательский server остаются proposal-направлением.

Evaluation runner фиксирует идентичность стратегии, набора заданий, workspace и валидатора, а также execution identity каждой попытки. Отдельный предметный этап — запустить `examples/route-dsl-benchmark` со штатным Route DSL validator и реальными обезличенными заданиями, получить baseline и сравнить модели или стратегии на неизменных fingerprints. OpenCode adapter реализован и может использоваться вместо Pi на уровне defaults, Markdown-команды или отдельного узла.

Подробности:

- [Состояние реализации](docs/05-implementation-status.md)
- [Аудит и исправления v0.1.2](docs/16-audit-remediation-v0.1.2.md)
- [Дополнительная стабилизация v0.1.3](docs/17-audit-remediation-v0.1.3.md)
- [Классификация parent loop v0.1.4](docs/18-audit-remediation-v0.1.4.md)
- [Восстановление документации v0.1.5](docs/19-document-recovery-v0.1.5.md)
- [Fake-assistant contract suite v0.1.6](docs/20-fake-assistant-contract-v0.1.6.md)
- [Усиление protocol contract v0.1.7](docs/21-protocol-hardening-v0.1.7.md)
- [Pi RPC adapter v0.1.8](docs/22-pi-adapter-v0.1.8.md)
- [Согласование Pi RPC-контракта v0.1.9](docs/23-pi-rpc-alignment-v0.1.9.md)
- [Усиление context/usage Pi v0.1.10](docs/24-pi-context-usage-hardening-v0.1.10.md)
- [OpenCode adapter v0.1.19](docs/33-opencode-adapter-v0.1.19.md)
- [Диагностика provider-сбоев OpenCode v0.1.20](docs/34-opencode-provider-diagnostics-v0.1.20.md)
- [Route DSL end-to-end v0.1.11](docs/25-route-dsl-e2e-v0.1.11.md)
- [Evaluation runner v0.1.12](docs/26-evaluation-runner-v0.1.12.md)
- [Изоляция и диагностика evaluation v0.1.13](docs/27-evaluation-isolation-report-v0.1.13.md)
- [Идентичность benchmark и качество v0.1.14](docs/28-benchmark-identity-quality-v0.1.14.md)
- [Семантика метрик и execution identity v0.1.15](docs/29-benchmark-metric-semantics-v0.1.15.md)
- [Семантика validation envelope v0.1.16](docs/30-quality-envelope-semantics-v0.1.16.md)
- [Разделение stdout/stderr quality-node v0.1.17](docs/31-quality-stdout-separation-v0.1.17.md)
- [Скилл настройки Takt v0.1.18](docs/32-takt-authoring-skill-v0.1.18.md)
- [Композиция workflow v0.1.22](docs/36-workflow-composition-v0.1.22.md)
- [Усиление композиции v0.1.23](docs/37-composition-hardening-v0.1.23.md)
- [Каталог процессов и умный роутер v0.1.24](docs/38-archon-workflow-catalog-v0.1.24.md)
- [Git worktree isolation v0.1.25](docs/39-git-worktree-isolation-v0.1.25.md)
- [Governed child Runs v0.1.26](docs/40-governed-child-runs-v0.1.26.md)
- [Политики возможностей узлов v0.1.27](docs/41-node-capability-policies-v0.1.27.md)
- [Динамический fan-out governed child Runs v0.1.28](docs/42-governed-child-fanout-v0.1.28.md)
- [Script-узлы и типизированные артефакты v0.1.29](docs/43-script-nodes-typed-artifacts-v0.1.29.md)
- [Локальный MCP control plane v0.1.30](docs/44-local-mcp-control-plane-v0.1.30.md)
- [Backlog v0.2](docs/14-backlog-v0.2.md)

## Документация

- [Краткие правила для кодовых агентов](AGENTS.md)
- [Скилл для настройки и использования Takt](skills/takt/SKILL.md)
- [Описание проекта](docs/01-project.md)
- [Подход к решению](docs/02-approach.md)
- [Текущая спецификация `takt/v1alpha1`](docs/03-specification.md)
- [Архитектура текущей реализации](docs/04-architecture.md)
- [Состояние реализации](docs/05-implementation-status.md)
- [Общий план развития](docs/06-roadmap.md)
- [Профиль совместимости с Archon](docs/07-archon-compatibility.md)
- [Целевое состояние v0.2](docs/08-target-v0.2.md)
- [Семантика runtime v0.2](docs/09-runtime-semantics.md)
- [Контракт адаптеров](docs/10-assistant-adapter-spec.md)
- [План реализации v0.2](docs/11-implementation-plan.md)
- [Карта источников истины](docs/12-document-map.md)
- [План оценки стратегий](docs/13-evaluation-plan.md)
- [Backlog v0.2](docs/14-backlog-v0.2.md)
- [Стартовая инструкция для кодового агента](docs/15-coding-agent-start.md)
- [Аудит и исправления v0.1.2](docs/16-audit-remediation-v0.1.2.md)
- [Дополнительная стабилизация v0.1.3](docs/17-audit-remediation-v0.1.3.md)
- [Классификация parent loop v0.1.4](docs/18-audit-remediation-v0.1.4.md)
- [Восстановление документации v0.1.5](docs/19-document-recovery-v0.1.5.md)
- [Fake-assistant contract suite v0.1.6](docs/20-fake-assistant-contract-v0.1.6.md)
- [Усиление protocol contract v0.1.7](docs/21-protocol-hardening-v0.1.7.md)
- [Pi RPC adapter v0.1.8](docs/22-pi-adapter-v0.1.8.md)
- [Согласование Pi RPC-контракта v0.1.9](docs/23-pi-rpc-alignment-v0.1.9.md)
- [Усиление context/usage Pi v0.1.10](docs/24-pi-context-usage-hardening-v0.1.10.md)
- [Route DSL end-to-end v0.1.11](docs/25-route-dsl-e2e-v0.1.11.md)
- [Evaluation runner v0.1.12](docs/26-evaluation-runner-v0.1.12.md)
- [Изоляция и диагностика evaluation v0.1.13](docs/27-evaluation-isolation-report-v0.1.13.md)
- [Идентичность benchmark и качество v0.1.14](docs/28-benchmark-identity-quality-v0.1.14.md)
- [Семантика метрик и execution identity v0.1.15](docs/29-benchmark-metric-semantics-v0.1.15.md)
- [Семантика validation envelope v0.1.16](docs/30-quality-envelope-semantics-v0.1.16.md)
- [Разделение stdout/stderr quality-node v0.1.17](docs/31-quality-stdout-separation-v0.1.17.md)
- [Скилл настройки Takt v0.1.18](docs/32-takt-authoring-skill-v0.1.18.md)
- [Композиция workflow v0.1.22](docs/36-workflow-composition-v0.1.22.md)
- [Усиление композиции v0.1.23](docs/37-composition-hardening-v0.1.23.md)
- [Каталог процессов и умный роутер v0.1.24](docs/38-archon-workflow-catalog-v0.1.24.md)
- [Git worktree isolation v0.1.25](docs/39-git-worktree-isolation-v0.1.25.md)
- [Governed child Runs v0.1.26](docs/40-governed-child-runs-v0.1.26.md)
- [Политики возможностей узлов v0.1.27](docs/41-node-capability-policies-v0.1.27.md)
- [Динамический fan-out governed child Runs v0.1.28](docs/42-governed-child-fanout-v0.1.28.md)
- [Script-узлы и типизированные артефакты v0.1.29](docs/43-script-nodes-typed-artifacts-v0.1.29.md)
- [Локальный MCP control plane v0.1.30](docs/44-local-mcp-control-plane-v0.1.30.md)
- [Граница безопасности](SECURITY.md)
- [JSON Schemas](schemas/README.md)

## Политики возможностей узла

AI-узлы поддерживают `allowed_tools`, `denied_tools`, `skills`, `mcp`, `sandbox` и `requires`. Явный `allowed_tools: []` означает запуск без инструментов. Adapter обязан объявить требуемые возможности до запуска; неподдерживаемая гарантия завершает узел до вызова модели. Политика сохраняется в состоянии, входит в fingerprint вместе с файлами MCP/skills и наследуется governed child Run как верхняя граница.

```yaml
- id: classify
  command: classify-change
  allowed_tools: []
  skills: []

- id: review
  command: review-code
  denied_tools: [edit, write]
  mcp: mcp/repository.json
  sandbox:
    filesystem: read_only
  requires: [tool_policy, mcp]
```

Filesystem/network policy текущей версии является assistant-enforced contract, а не OS sandbox. `process` получает policy через протокол и `TAKT_POLICY_JSON`; Pi/OpenCode применяют только реально поддерживаемые встроенные capabilities.

## Важная граница

Takt управляет процессом снаружи. Внутренний цикл инструментов, работа с файлами, MCP, LSP, история сообщений и сжатие контекста остаются ответственностью Pi, OpenCode или другого кодового агента.

## Готовый режим разработки по Markdown-плану

```bash
takt init code
takt validate code --workspace . --json
takt run code --workspace . --input docs/plan.md --json
```

Профиль `code` устанавливается в `.takt/profiles/code/`. Markdown-файл остаётся исходным планом; Takt передаёт агенту путь и содержимое без обязательного преобразования в отдельную структуру задач.
