# Спецификация `takt/v1alpha1`

Статус: текущий реализованный внешний контракт `v0.1.57-alpha` с единым
Archon-first языком Workflow A0 и bounded repair/runtime semantics A1.
Config, Profile, Run и assistant protocol сохраняют собственные versioned
контракты. Машиночитаемые схемы находятся в `schemas/`.

## 1. Область применения

Текущая реализация рассчитана на локальный однопользовательский trusted runtime. Workflow, config, Markdown-команды, shell-команды и рабочая директория считаются доверенными.

## 1.1. Локальный MCP control plane

Команда `takt mcp --surface agent --workspace <dir> [--config <path>]` запускает одноразовый stdio JSON-RPC/MCP adapter поверх общего control service. `takt mcp --daemon` проксирует тот же stdio-протокол в локальный `takt daemon` через Unix socket. БД и сетевой listener не создаются.

Поддерживаются два протокольных входа:

- legacy `initialize` с версиями MCP 2025;
- stateless `server/discover` с `protocolVersion: 2026-07-28`.

Полная совместимая поверхность публикует 54 операции, разделённые на `agent|host|worker|operator|all`. Поверхность `agent` является default и содержит только пять `takt.task.start|status|respond|stop|explain`; host-control, notification delivery и внешний executor/tool-call lifecycle скрыты от основной LLM. `takt.run.start` по умолчанию отсоединяет запуск и возвращает устойчивый `run_id`. События читаются по `revision` cursor, а содержимое артефактов выдаётся только по явному запросу с ограничением размера.

MCP и daemon являются локальными интерфейсами текущего пользователя. Они не добавляют sandbox или новые полномочия и не предназначены для сетевой публикации. Полный control contract зафиксирован в `44-local-mcp-control-plane-v0.1.30.md`, внешний executor и события — в `45-agent-events-external-executor-v0.1.31.md`, а daemon и authoring preflight — в `47-authoring-local-daemon-v0.1.33.md`.

## 1.2. Authoring preflight

`takt validate` проверяет неизвестные поля с path-aware `did you mean`,
command/model/provider references, effective adapter capabilities, статические
`$<node>.*`/approval/artifact references и несовместимые параметры. Diagnostics
возвращаются в JSON; `--warnings-as-errors` делает предупреждения ошибками CI.

Единый renderer использует формы `$path` — обязательная ссылка, `$path?` —
optional, `$path:-default` — явное значение по умолчанию. Неразрешённая
обязательная ссылка является ошибкой и не передаётся действию как буквальный
текст. Старые `${...}` и `$USER_MESSAGE` не являются вторым dialect и
отклоняются до создания Run.

## 1.3. Локальный daemon

`takt daemon start|status|stop` управляет локальным процессом одного workspace. Daemon слушает `.takt/daemon.sock`, использует тот же файловый Store и поддерживает background `takt run --daemon`, `takt events --daemon --follow` и `takt mcp --daemon`. Несколько клиентов одного пользователя сериализуют короткие изменения Run через существующий file lock с bounded retry. Daemon переживает закрытие клиента. После перезапуска он обнаруживает локальные `running|pausing` Run с мёртвым executor PID, помечает незавершённую attempt как `worker_lost`, возвращает узел в `pending` и продолжает граф. Это PID-based recovery и не гарантирует отсутствие повторного внешнего side effect.

## 2. Файловая структура

```text
.takt/
  config.yaml
  commands/
  workflows/
  runs/
  host-sessions/
  notifications.yaml
  notifications/
```

Порядок поиска команд:

1. `<workspace>/.takt/commands/`;
2. `commands/` рядом с workflow;
3. для подключённого или профильного workflow — `commands/` в каждом родительском каталоге до корня композиции/профиля;
4. `~/.takt/commands/`.

## 3. Конфигурация моделей и исполнителей

```yaml
apiVersion: takt/v1alpha1
kind: Config
default_assistant: pi

model_preset: qwen36
model_presets:
  qwen36:
    implementation: aihub/Qwen/Qwen3.6-27B
    review: aihub/Qwen/Qwen3.6-27B
    routing: aihub/Qwen/Qwen3.6-27B

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
- `process` — универсальный текстовый или JSON process adapter, включая внешние Codex/Oh My Pi/Qwen CLI wrappers;
- `pi` — специализированный adapter для `earendil-works/pi` через `pi --mode rpc`;
- `opencode` — специализированный JSON CLI adapter OpenCode.

Профиль может ссылаться на логическое имя `coding-agent`; оно разрешается через `default_assistant`. Ядро Takt не зависит от Kiro CLI.

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

При `protocol: takt-assistant/v1alpha1` stdin содержит строгий JSON request envelope, а stdout — ровно один JSON result envelope. `takt-assistant/v1alpha2` использует тот же request boundary, но stdout является NDJSON event stream с ровно одним terminal result event. Runtime проверяет версию, type, status, обязательный `exit_code`, отсутствие неизвестных полей, неотрицательные usage-метрики и session resume. OS exit code и terminal `result.exit_code` обязаны совпадать всегда, включая ноль; расхождение классифицируется как `protocol`. Невалидный, обрезанный или дополнительный terminal result также является `protocol`, а отказ resume не превращается в fresh. Схема находится в `schemas/assistant-protocol.schema.json`.

Переменные окружения process assistant включают `TAKT_RUN_ID`, `TAKT_NODE_ID`, `TAKT_ATTEMPT`, модель, workspace, session и native hooks.

`max_output_bytes: 0` означает отсутствие лимита. При превышении положительного лимита output обрезается, а NodeState получает `output_truncated: true`.

На Unix процесс запускается в отдельной process group; timeout и cancellation завершают процесс и его потомков.

### Доменные адаптеры SCM, tracker и CI

В `Config` поле `adapters` задаёт нейтральные интеграции с инженерной средой. Workflow ссылается на имя адаптера и предметную операцию, а не на GitHub, GitLab, Jira или конкретную корпоративную систему.

```yaml
adapters:
  scm:
    domain: scm
    transport: mcp
    argv: [corp-scm-mcp]
    operations:
      change.create: corp_change_create
    reconcile_operations:
      change.create: corp_change_reconcile
    timeout: 30s

  tracker:
    domain: tracker
    transport: process
    argv: [corp-tracker-adapter]
```

Допустимые домены — `scm`, `tracker`, `ci`; транспорты — `process` и `mcp`. Операция использует lowercase dot-separated имя (`change.create`, `item.get`, `run.start`). `process` реализует `takt-domain-adapter/v1alpha1`; MCP transport выполняет `initialize`, `tools/list` для capability discovery и `tools/call`. Для MCP `operations` и `reconcile_operations` связывают нейтральные операции с именами конкретных tools; без явной карты используется `<domain>.<operation>`.

Перед выполнением runtime запрашивает capabilities и fail-fast отклоняет обязательную неподдерживаемую операцию. `takt adapter list|describe|doctor` позволяет проверить конфигурацию и фактическую декларацию адаптера без запуска workflow. Схемы: `schemas/config.schema.json` и `schemas/domain-adapter-protocol.schema.json`; публичные типы и core operation names находятся в `sdk/domainadapter`.

## 3.0. Входной контракт workflow

Workflow может объявить `input.format: json` и строгую JSON Schema в `input.schema`. До создания Run Takt декодирует вход, отклоняет неизвестные поля и применяет проверяемый subset (`type`, `properties`, `required`, `additionalProperties`, `enum`, `items`, `minItems`/`maxItems`, `uniqueItems`, `minLength`/`maxLength`, `pattern`, `minimum`/`maximum`, `minProperties`/`maxProperties`, integer semantics), общий со structured output. Профиль может задать JSON input отдельно для каждого workflow.

Это используется шестью основными процессами профиля `code` 0.17.0: issue/idea/plan/review/PIV/Ralph входы проверяются до вызова assistant и изменения Git workspace.

### Детерминированный контракт `code:plan-to-pr`

`code:plan-to-pr` требует поля `repository`, `plan_path`, `base_branch`,
`draft_pr`, непустой `validation_commands` и непустой уникальный
`allowed_paths`. Последний содержит repository-relative non-magic Git pathspec:
leading `:`, absolute/volume-prefixed пути, пустые значения и сегмент `..`
запрещены.

Scope gate сравнивает фактические изменения с merge-base `HEAD` и
`base_branch`. Tracked/staged/worktree пути читаются через
`git diff --name-only -z --no-renames`, untracked — через
`git ls-files --others --exclude-standard -z`; matching выполняет Git. Gate
работает после deterministic validation и повторно после review fixes.

Draft PR создаётся только после validation/scope gates. Затем отдельные
deterministic gates требуют `PR_READY`, принятый review block, повторную
validation/scope-проверку и `WORKFLOW_COMPLETE`. Успешный root Run заканчивается
exact output `WORKFLOW_ACCEPTED`; любой отсутствующий artifact или доменный
неуспех даёт failed Run. Это не является независимым подтверждением remote PR:
assistant/`gh` receipt остаётся фазовым evidence без SCM reconcile.

## 3.1. Внешний исполнитель AI-узла

`command` и `prompt` поддерживают `executor: external`. Runtime разрешает команду, шаблоны, model/session и effective policy, затем сохраняет durable external task и переводит Run в `waiting` с `kind: external_node`. Worker получает задачу через MCP, заявляет capability declaration и lease. Claim token не входит в публичную проекцию Run.

Event protocol v2 использует: `assistant.session.started`, `assistant.session.resumed`, `assistant.message`, `assistant.tool.requested`, `assistant.tool.allowed`, `assistant.tool.denied`, `assistant.tool.started`, `assistant.tool.completed`, `assistant.artifact.declared`, `assistant.usage`, `assistant.diagnostic`, `assistant.completed`, `assistant.failed`. Raw stdout/stderr сохраняются отдельно.

При `tool_approval` worker обязан запросить tool call до фактического запуска. Takt сначала применяет effective node policy, затем при необходимости сохраняет blocking approval. Запуск разрешён только после `allow`. Отмена одного tool call сохраняется отдельно от отмены Run. Артефакт внешнего worker регистрируется через `takt.node.artifact.declare` и связывается с устойчивым `call_id`. Внешний узел нельзя завершить, пока tool call не достиг terminal-состояния `completed|failed|denied|cancelled`. После `takt.node.complete|fail` результат проходит обычные `output_format`, attempts, hooks и artifact semantics.

Встроенные adapters используют тот же нормализованный event contract через `assistant.Request.Emit`, но заявляют только реально доступные capabilities. Наблюдательные tool events OpenCode/Pi не означают pre-execution `tool_control`.

### Контракт side_effect

`side_effect` допустим у `executor: external` и у доменного `adapter`-узла. Он описывает семантику внешнего изменения, а не конкретный провайдер:

```yaml
side_effect:
  mode: reconcile
  idempotency_key: stable-change-key
```

- `idempotent` означает, что повтор с тем же ключом безопасен по контракту исполнителя;
- `reconcile` означает, что после неопределённого результата повтор запрещён до сверки факта;
- если `idempotency_key` не задан, Takt использует устойчивый ключ `run_id:node_id`;
- `applied` завершает операцию только с receipt/result;
- `not_applied` разрешает один безопасный повтор с тем же ключом;
- `unknown` сохраняет неопределённое состояние и запрещает blind retry.

Для внешнего worker сверка выполняется через `takt.node.reconcile`. Для доменного adapter runtime вызывает его `Reconcile` автоматически; при `side_effect.mode: reconcile` соответствующая capability проверяется **до первого вызова операции**. Состояние ключа, receipt и reconcile outcome сохраняется durable в Run state. YAML-схема находится в `schemas/workflow.schema.json`, сохранённое состояние — в `schemas/run-state.schema.json`.

### Pi assistant

`type: pi` использует официальный RPC-режим Pi. Takt запускает отдельный процесс на попытку узла, запрашивает состояние сессии и накопленную статистику, отправляет prompt через JSONL RPC и ждёт финальное событие `agent_settled`. Событие `agent_end` считается границей одного низкоуровневого запуска и не завершает попытку, если Pi выполняет автоматический retry, compaction retry или queued continuation. После `agent_settled` adapter читает итоговый текст, сообщения, повторную статистику и Session ID. Фактическая модель берётся из `responseModel` последнего assistant message с fallback на выбранную модель сессии; результат version probe также сохраняется в `NodeState`. Затем adapter закрывает stdin для штатного завершения процесса.

Поля конфигурации:

- `binary` — путь к `pi`, по умолчанию `pi` из `PATH`;
- `args` — дополнительные нерезервированные параметры Pi;
- `session_dir` — каталог сессий, передаётся как `--session-dir`;
- `project_trust` — `default`, `approve` или `deny`; последние два соответствуют `--approve` и `--no-approve`;
- `settings` — нативный JSON-объект Pi без переименования ключей; при создании
  изолированного рабочего каталога оценки он становится содержимым
  `.pi/settings.json`;
- `env` — дополнительные переменные окружения;
- `max_output_bytes` — общий лимит RPC stdout и stderr; при нуле используется безопасный лимит adapter по умолчанию. Если timeout или cancellation совпали с переполнением, причина context сохраняет классификацию `timed_out` или `cancelled`, а truncation остаётся диагностическим признаком.

Настройки таймаутов самого Pi (`httpIdleTimeoutMs` и
`retry.provider.timeoutMs`) можно передать в `settings`; они описаны в
[спецификации адаптера Pi](10-assistant-adapter-spec.md#9-pi-adapter).
Они не меняют Takt `timeout` (общий deadline попытки) и `idle_timeout`
(отсутствие нормализованной активности узла): каждый из этих лимитов может
завершить выполнение независимо.

Takt сам задаёт `--mode rpc`, `--provider`, `--model`, `--thinking`, `--session` и параметры trust/session directory. Эти флаги запрещены в `args`, чтобы исключить расхождение структурированного Request и фактического запуска.

`model.provider` и `model.id` должны соответствовать каталогу моделей Pi. Параметры `thinking` или `reasoning_effort` переводятся в `--thinking`; остальные model params доступны расширениям через `TAKT_MODEL_PARAMS_JSON`, но не интерпретируются adapter.

При `session: resume` adapter передаёт `--session <id>` и проверяет через `get_state`, что Pi действительно открыл тот же Session ID. Тихий переход на fresh запрещён. В режиме `fresh` сохранённый ID не передаётся.

Pi `stopReason: length` не является успешным settled-result: adapter возвращает
execution kind `exit`, сохраняя Session ID и usage. Встроенный
`code:feature-development` повторяет такой `exit` до трёх попыток с
`retry_session: reuse`; следующий prompt получает feedback о достигнутом output
limit. Per-response `maxTokens` принадлежит Pi model registry и не совпадает ни
с `contextWindow`, ни с byte-limit `max_output_bytes` Takt adapter.


Статистика `get_session_stats` является накопленной по всей сессии. Adapter снимает её до prompt и после `agent_settled`, а в `Result.Usage` записывает неотрицательную дельту текущей попытки. Уменьшение накопленных значений или исчезновение usage из второго снимка после его наличия в первом классифицируется как `protocol`. Явные нулевые значения валидны. Полные снимки сохраняются в structured result как `stats_before` и `stats_after`.

`metadata` остаётся необязательным полем внутреннего Request. Текущий workflow runtime его не формирует, однако Pi adapter прозрачно передаёт заполненное значение через `TAKT_METADATA_JSON`. `native_hooks` передаются через `TAKT_NATIVE_HOOKS_JSON`; автоматического преобразования в Pi extensions в `v1alpha1` нет.

Интерактивные запросы Pi extension UI (`confirm`, `select`, `input`, `editor`) отклоняются как `protocol`: Takt approval должен быть отдельным сохраняемым узлом workflow. Fire-and-forget события `notify`, `setStatus`, `setWidget`, `setTitle` и `set_editor_text` допускаются и не требуют ответа.

### OpenCode assistant

`type: opencode` запускает `opencode run --format json` в workspace узла. `binary`, `args`, `agent`, `auto_approve`, `env` и `max_output_bytes` задаются в config. `argv`, `protocol`, `session_dir` и `project_trust` для этого типа запрещены.

Takt передаёт выбранную модель как `<provider>/<id>`, `params.variant` как `--variant`, prompt через stdin, а сохранённый Session ID — через `--session`. При resume возвращённый event stream обязан содержать тот же Session ID. Stdout содержит NDJSON events; stderr остаётся диагностическим. Usage одной попытки является суммой уникальных `step_finish`; event `error` является отказом агента независимо от OS exit code.

При timeout/cancellation итоговая классификация остаётся `timed_out`/`cancelled`. Доступные сообщения о provider retry, соединении и error events сохраняются в raw stdout/stderr, logical output и тексте ошибки узла. Общая проверка attempt context не заменяет более содержательную context-ошибку OpenCode на общее сообщение.

`auto_approve: true` включает OpenCode `--auto` и предназначен только для доверенной рабочей директории.


## Dynamic Takt

Высокоуровневый `takt plan`/`takt.plan` возвращает решение `existing|planned`. `planned` использует `takt/v1alpha1 WorkflowPlan`: цель, жёсткие budgets, упорядоченные фазы `task|map`, зависимости, источник map и явные checkpoint. `uses` обязан ссылаться на разрешённый блок профиля. Начиная с v0.1.43 phase может содержать `repository` и `publish_change`; repository обязан существовать в Workspace catalog, его dependency graph должен быть отражён зависимостями фаз, а один repository имеет не более одного mutating owner phase. План проходит строгую проверку и компилируется в обычный `takt/v1alpha1 Workflow`; отдельная runtime-семантика для WorkflowPlan отсутствует.

`takt execute` требует подтверждение planned-плана. Перепланировщик вызывается только после checkpoint и возвращает `continue|replace_remaining|finish|ask_user`. `replace_remaining` создаёт новую revision и не изменяет завершённые фазы. `takt steer` сохраняет уточнение до ближайшего checkpoint. Completed planned-план может быть продвинут через `takt plan promote` в `.takt/workflows/generated/` после повторной загрузки и Validate.

### Доверенные пакеты блоков

Профиль может объявить `block_packages`: список локальных `takt/v1alpha1 BlockPackage`. Начиная с `v0.1.42-alpha`, те же пакеты можно устанавливать через `takt package`; locked packages подключаются к профилю автоматически и не требуют ручного изменения `block_packages`. Каждый пакет содержит блоки с относительным workflow path, capabilities, integrations, `output_paths`, checks/policy, templates, roles и governance.

Устанавливаемые scope: `global`, `corporate`, `project`. При совпадении имени блока действует `project > corporate > global > builtin`; governance всех пакетов по-прежнему объединяется fail-closed. Global content хранится в `~/.takt/packages/global`, project/corporate — в `<workspace>/.takt/packages/<scope>`.

Пакет может объявить `dependencies` с version constraint и `requirements.takt`, а также `requirements.adapters` с именем/domain, обязательными operations, reconcile capabilities и уровнем `required|preferred`. Required adapter requirements проверяются до Run; preferred capability передаётся Task Router/Planner как недоступная возможность и может быть исключена из плана без позднего падения. Автоматическая установка зависимостей в `v0.1.42` не выполняется: dependency graph проверяется до install/update/uninstall.

Local/Git sources фиксируются в `.takt/takt.lock.json` (global — `~/.takt/takt.lock.json`). Lock хранит точную версию, source/ref, Git commit, SHA-256 содержимого и результат проверки подписи. Перед автоматическим подключением locked packages к профилю integrity/policy проверяются повторно; drift установленного дерева fail-closed отклоняет запуск до `package doctor|sync`. `takt package sync` восстанавливает Git content по commit; local source обязан воспроизводить locked version/checksum. Опциональный `PackagePolicy` в `.takt/package-policy.yaml`/`~/.takt/package-policy.yaml` ограничивает source prefixes и может требовать Ed25519 package signature для выбранных scopes.

Начиная с v0.1.39-alpha пакет также может объявлять внутренние `roles`. Блок связывается с ролью и получает bounded `TaskBrief`, context recipe, `expected|allowed|protected|forbidden` scope и `checks` с `required|preferred` + `deny|repair|warn`. Эти роли не являются глобальными агентами кодинг-хоста. Машиночитаемые схемы: `schemas/block-package.schema.json`, `schemas/task-brief.schema.json`, `schemas/package-lock.schema.json`, `schemas/package-policy.schema.json`, `schemas/package-signature.schema.json`.

Каталог загружается до планирования. Workflow блока проходит обычный Load/Validate, обязан иметь один публичный terminal output и не может запускать governed child Run. Каждый `output_path` обязан существовать в terminal `output_format`; источник `map` должен точно совпасть с объявленным путём типа `array`. Fingerprint манифеста и транзитивного исполняемого содержимого сохраняется в плане и проверяется до execute/replan/promote.

Зависимость пакета может указывать `scope: global|corporate|project`; без scope одноимённые пакеты в нескольких scopes считаются неоднозначными и проверка завершается ошибкой. Ограничение `^` использует semver compatible-update semantics: для `0.x` фиксируется minor (`^0.1.42` принимает `0.1.x`, но не `0.2.0`), а для `0.0.x` фиксируется patch.

### Workspace и multi-repo

`.takt/workspace.yaml` обязан явно содержать `apiVersion: takt/v1alpha1`, `kind: Workspace` и список `repositories` с `id`, относительным `path` и необязательным `depends_on`. Repository ID соответствует `[a-z][a-z0-9-]{0,62}`. Путь обязан оставаться внутри control workspace после symlink resolution и вести в Git repository; IDs уникальны, dependency graph ацикличен. При отсутствии manifest Takt выполняет bounded discovery одного root Git repository либо immediate child repositories. Схема — `schemas/workspace.schema.json`.

Governed child `workflow` может задать `repository`: относительный repository path внутри общего control workspace. Такой child не может использовать `isolation: inherit`; mutating multi-repo phase использует отдельный `worktree`. Parent NodeState сохраняет `child_control_workspace`, `child_execution_workspace`, `child_branch`, `child_base_commit`, а Dynamic Plan сохраняет per-repository candidate/evidence.

CLI пакетов: `takt package install|update|uninstall|list|sync|doctor|sign`. CLI каталога: `takt block list|describe|validate`. MCP для authoring сохраняет `takt.block.list|describe`; package mutation намеренно остаётся операторской CLI-операцией и не добавляет новые public agent tools.

## 4. Markdown-команды

```markdown
---
description: Исправляет реализацию по результатам проверки
provider: pi
model: large
---

Исправь проект.

Запрос пользователя:
$ARGUMENTS

Результат предыдущей проверки:
$FEEDBACK
```

Frontmatter поддерживает `description`, `provider`, `model` и
`argument-hint`. Legacy key `assistant` отклоняется. Остальные поля сохраняются
как метаданные. Приоритет binding: `node.provider` → frontmatter `provider` →
`workflow.provider`; неизвестный binding или model останавливает authoring до
создания Run.

## 5. Workflow

```yaml
name: example
description: bounded repair flow
provider: pi
model: large

nodes:
  - id: implement
    command: implement
    timeout: 10m
    attempts:
      max: 3
      retry_on: [exit, timed_out]
      backoff:
        initial: 1s
        multiplier: 2
        max: 15s
        jitter: true
    hooks:
      after_node:
        - id: validate
          bash: go test ./...
          on_failure:
            action: retry
            session: resume

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

Root Workflow принимает `name`, `description`, `labels`, `provider`, `model`,
`nodes` и существующие Takt extensions (`hooks`, `worktree`, `input`).
`apiVersion`, `kind`, `metadata`, `defaults` и node field `assistant` относятся
к старому dialect и fail-closed отклоняются. Каждый node обязан иметь ровно
одно действие; `provider` и `context` (`fresh` по умолчанию или `shared` в A1)
являются декларативными defaults для assistant node.

`timeout` использует формат Go duration: `500ms`, `30s`, `5m`, `1h` и ограничивает всю попытку узла. `idle_timeout` поддерживается AI-узлами и сбрасывается нормализованными событиями активности; для claimed внешнего узла его обслуживает daemon. `always_run: true` запускает cleanup-узел после terminal-состояния всех зависимостей независимо от их результата, но не скрывает failure основного графа.

`attempts.retry_on` задаёт execution kinds, для которых разрешён автоматический повтор (`exit|start|protocol|internal|timed_out`). Cancellation и неизвестный внешний side effect не являются обычным retry. `attempts.backoff` требует `attempts.max >= 2`: `initial` и `max` — положительные Go duration, `multiplier` по умолчанию 2 и не меньше 1, `jitter` выбирает задержку в диапазоне 50–100% от расчётной. Runtime сохраняет выбранный `not_before` в `NodeState.retry`, поэтому restart/resume не пересчитывает уже принятое ожидание.

Классы execution error также включают внутренний adapter kind `provider_unavailable`; он не является значением `attempts.retry_on` и обрабатывается отдельным provider retry scope ниже.

`provider_unavailable` — внутренний failure kind assistant adapter, а не новое
YAML-поле и не значение `attempts.retry_on`. Явное сообщение провайдера
`connection error` считается эквивалентной transient transport evidence наравне
с connection reset/refused. Takt сам делает ровно до трёх
вызовов `SessionAdapter.Run` для одной workflow-попытки (первый вызов и два
resume); внутренние automatic retries Pi/OpenCode в этот лимит не входят.
Повтор использует тот же Session ID, delays `2s`, затем `4s`, либо прямой
`Retry-After`, ограниченный `60s`. Provider-попытки сохраняются отдельно от
`attempts.max`; после исчерпания Run завершается с `provider_unavailable`, и
`allow_failure` его не принимает. Этот retry относится только к assistant
model invocation: domain adapter side effect, включая `side_effect: reconcile`,
его не использует. Исходный абсолютный deadline `timeout` сохраняется в durable
provider marker: backoff и все resume входят в ту же workflow-попытку и не
получают новый полный timeout после restart/resume.

Каждая неуспешная execution получает machine-readable `diagnostic` с `code`, `kind`, `op`, исходным `message`, стабильным `fingerprint` и `retryable`. Fingerprint нормализует workspace path и volatile numbers и сохраняется отдельно для каждой `ExecutionState`; LLM similarity в этом контракте не используется.

Независимые готовые узлы `command`, `prompt` и `bash` без portable hooks и повторных попыток выполняются одной параллельной волной. Переходы `pending → running → terminal` и запись событий сериализуются, поэтому Run и журнал остаются едиными и детерминированными. Узлы с hooks или `attempts.max > 1` пока исполняются последовательно.

`NodeState.path` хранит каноническую структурированную идентичность (`/build`, `/batch[1]/append`) и публикуется как `node_path` в node events. Совместимый `NodeState` key/ID не меняется: path добавляет устойчивую namespace-модель поверх существующих скомпилированных `__` IDs.

## 6. Типы узлов

### `command`

Загружает Markdown-команду и запускает assistant.

### `prompt`

Передаёт assistant встроенный prompt.

Для `command`, `prompt` и `script` можно задать проверяемый JSON-контракт `output_format`. `input.schema` и `output_format` используют один версионированный контракт `takt-schema-subset/v1`, **не полный JSON Schema**. Takt проверяет допустимый subset при authoring, а семантику проверки JSON-значения выполняет `github.com/santhosh-tekuri/jsonschema/v6` в режиме Draft 2020-12; собственного JSON Schema runtime в Takt нет. Поддерживаются `type`, `description`, `properties`, `required`, строковый `enum`, `items`, `minItems/maxItems/uniqueItems`, `minLength/maxLength/pattern`, `minimum/maximum`, `minProperties/maxProperties` и boolean `additionalProperties`. `$ref`, `oneOf/anyOf/allOf`, `const/default/format` и schema-valued `additionalProperties` не поддерживаются. Runtime принимает ровно одно JSON-значение и сохраняет канонический компактный JSON. Нарушение контракта завершает узел ошибкой `protocol`. Машиночитаемая граница доступна через `takt compatibility schema`.

```yaml
- id: classify
  prompt: Классифицируй запрос и верни только JSON.
  output_format:
    type: object
    properties:
      workflow:
        type: string
        enum: [assist, fix-github-issue]
      reason:
        type: string
    required: [workflow, reason]
    additionalProperties: false
```


### Политики `command` и `prompt`

```yaml
- id: classify
  command: classify-change
  allowed_tools: []
  skills: []

- id: review
  command: review-code
  denied_tools: [edit, write]
  skills: [skills/go-review]
  mcp: mcp/repository.json
  sandbox:
    filesystem: read_only
    network: deny
  requires: [tool_policy, skills, mcp]
```

Поля поддерживаются только у `command` и `prompt`:

- `allowed_tools` — верхняя граница доступных инструментов; явный пустой список означает отсутствие инструментов;
- `denied_tools` — дополнительный deny-list; пересечение с allowlist запрещено;
- `skills` — список имён или путей; явный пустой список запрещает inherited skills;
- `mcp` — JSON-файл конфигурации относительно workflow;
- `sandbox.filesystem: read_only` и `sandbox.network: deny` — гарантии assistant adapter, когда `enforcement` не задан;
- `requires` — дополнительные capability names.

До запуска assistant runtime вычисляет эффективную политику, проверяет `Adapter.Capabilities()`, сохраняет её в `NodeState.policy` и передаёт adapter. Неподдерживаемая capability завершает узел до запуска процесса. Для governed child Run поле `workflow.policy` задаёт inherited upper bound. Allowlist и skills пересекаются, deny/requirements объединяются, а более строгая sandbox-политика наследуется. Файлы MCP и локальные skills входят в fingerprint.

Для `command/prompt` это assistant-enforced contract, а не OS sandbox. `process` обязан объявить поддерживаемые capabilities в config и получает политику через `takt-assistant/v1alpha1`/`v1alpha2` и `TAKT_POLICY_JSON`. Pi поддерживает tool policy, path skills и read-only tool restriction. OpenCode получает permission/MCP config через `OPENCODE_CONFIG_CONTENT`; локальные path skills дополнительно внедряются в prompt. Network deny не объявляется bundled Pi/OpenCode extensions и потому отклоняется до запуска.

Реальный локальный OS wrapper доступен только для `bash` и `script`, которыми Takt управляет напрямую:

```yaml
sandbox:
  enforcement: required   # required | optional
  filesystem: read_only
  network: deny
```

Linux использует `bwrap`, macOS — `sandbox-exec` при наличии. `required` без backend завершает node до запуска payload; `optional` выполняет node без системной изоляции, но фиксирует `NodeState.sandbox.status: degraded`. `runtime: validation` и hooks проходят через ту же node-level политику. OS `enforcement` у `command/prompt` отклоняется: Takt не может честно обернуть внутренние tool calls чужого coding-agent.

### `bash`

Выполняет команду через `bash -lc`. Runtime сохраняет stdout и stderr раздельно в `stdout`/`stderr`, а также формирует объединённый `output` для шаблонов, feedback и диагностики.

`allow_failure: true` разрешает только штатный ненулевой exit code. Ошибка запуска, timeout, cancellation или ошибка runtime остаются ошибкой узла.

### SecretRef для process/script environment

Значение вида `secret://ENV_NAME` разрешается из окружения непосредственно перед запуском process assistant, process domain adapter или `script.env`. Отсутствующий explicit secret завершает запуск fail-closed. Известные secret values редактируются перед записью durable state/events и textual artifacts; non-text artifact с известным secret отклоняется. Foreground control/CLI возвращает повторно загруженный durable state, а не transient live state. Takt не хранит секреты и не заменяет Vault/Keychain. Для resume используйте SecretRef, а не literal secret в task input.

### `script`

Запускает детерминированный скрипт без assistant:

```yaml
- id: index
  script:
    runtime: command
    path: tools/build-index
    args: [--json]
    env:
      MODE: strict
    dependencies: [schemas/index.schema.json]
  output_format:
    type: object
    properties:
      files:
        type: array
        items: {type: string}
    required: [files]
  output_type: index
  output_mime: application/json
```

`runtime` принимает `command`, `python` или `node`. `command` требует `path`; `python` и `node` принимают ровно одно из `path` и `inline`. Дополнительно доступны `args`, `env`, `working_directory` и `dependencies`. Пути вычисляются относительно workflow и отображаются в execution workspace при managed worktree. Runtime передаёт `TAKT_RUN_ID`, `TAKT_NODE_ID`, `TAKT_ATTEMPT`, `TAKT_WORKSPACE` и `TAKT_ARTIFACTS_DIR`. Inline source передаётся интерпретатору byte-for-byte: Takt references в нём запрещены authoring-валидацией; значения передаются только через `args` или `env`.

Stdout/stderr сохраняются раздельно. `output_format` нормализует только `Output`, не затирая raw stdout. Исходник script и файлы `dependencies` входят в fingerprint.

### `adapter`

Выполняет одну нейтральную операцию через доменный адаптер из `Config`:

```yaml
- id: create-change
  adapter:
    name: scm
    operation: change.create
    input: |
      {"title":"$prepare.output.title"}
  side_effect:
    mode: reconcile
    idempotency_key: create-change
  output_format:
    type: object
```

`adapter` является обычным действием Node и проходит тот же scheduler, dependencies, attempts, hooks, timeout и `output_format`. Он не создаёт второй runtime. `adapter.input` после template rendering обязан быть JSON object. Runtime выполняет capability preflight, а в `NodeState.domain_operation` сохраняет домен, операцию, обнаруженные capabilities, idempotency key, receipt и состояние reconcile.

### Типизированные артефакты

`command`, `prompt`, `bash` и `script` могут объявить `output_type`, `output_mime` и `output_path`. Если `output_path` отсутствует, сохраняется нормализованный `Output`; если указан, файл копируется из execution workspace либо `$TAKT_ARTIFACTS_DIR` в хранилище Run. Ссылка содержит type, MIME, SHA-256, size, producer Run/Node, attempt и timestamp.

```yaml
- id: plan
  command: create-plan
  output_type: plan
  output_mime: text/markdown
  output_path: $ARTIFACTS_DIR/plan.md

- id: implement
  depends_on: [plan]
  prompt: |
    Реализуй план из файла:
    $plan.artifacts.plan.path
```

Доступны `$<id>.artifacts.<type>.path`, `.sha256`, `.mime`, `.size`, producer
metadata и другие именованные поля. Числовой artifact type/index запрещён.
Governed child Run и fan-out поднимают ссылки родителю, сохраняя producer
provenance.

### `approval`

Переводит Run и Node в `waiting`. Ответ сохраняется как output узла при `capture_response: true`. Остановка на approval не расходует попытку.

### `loop_group`

Повторяет вложенный DAG до выполнения условия или достижения `max_iterations`. Значение `max_iterations` должно находиться в диапазоне `1..64`. Дочерний DAG использует те же `depends_on`, `when`, `trigger_rule`, hooks и классификацию ошибок, что и корневой workflow.

```yaml
until:
  node: validate
  exit_code: 0
  signal: BUILD-CLEAN
  requires:
    - node: tests
      exit_code: 0
until_bash: test -s "$validate.artifacts.report.path"
```

Также поддерживается компактная форма одного assistant node:

```yaml
- id: repair
  loop:
    prompt: Исправь результат проверки и выведи сигнал в конце.
    until: BUILD-CLEAN
    max_iterations: 3
    fresh_context: false
```

`until` допускает `exit_code`, `output_contains`, ровно один ожидаемый
`signal` и список `requires` с независимыми доказательствами. Signal считается
только при единственном валидном `<promise>NAME</promise>` или последней
непустой строке `NAME`; fenced Markdown исключается, обрезанный output даёт
protocol failure. `until_bash` выполняется как детерминированный predicate и
сохраняет stdout/stderr, exit code, duration, truncation и error code в
`PredicateEvidence`. Условие `until` проверяется только для дочернего узла со
статусом `completed`; `skipped`, `failed`, `errored`, `timed_out` и `cancelled`
не завершают цикл даже при совпадающем нулевом `exit_code`.

Failure-like body nodes останавливают loop до вычисления predicate и не могут служить acceptance evidence. При protocol/start/timeout/cancel ошибке predicate активные child states сначала сохраняются в immutable iteration snapshot с satisfied: false, затем очищаются из transient state; resume продолжает следующую bounded итерацию без потери доказательств.

Если timeout или cancellation родительской попытки наступают во время выполнения дочернего узла, родительский `loop_group` и Run сохраняют `timed_out` или `cancelled`. Производная ошибка `loop_group exhausted` не переопределяет причину завершения контекста.

После каждой завершённой итерации runtime сохраняет immutable snapshot в `NodeState.loop_iterations[]`. `loop_previous` остаётся совместимым представлением последней завершённой итерации. История доступна через обычный Run state/MCP `run.get`; внутренние expanded IDs скрываются из public view.

`subworkflow`, `foreach` и `approval` разрешены внутри `loop_group` и используют тот же дочерний DAG. При остановке на approval сохраняется активная итерация; `takt answer` продолжает её, а следующая итерация создаёт новый запрос approval. Поле `until.node` ссылается на публичный ID контейнера. `fresh_context: true` начинает следующую итерацию без Session ID; по умолчанию assistant nodes продолжают предыдущий Session ID и требуют exact resume. `context: shared` разрешает downstream assistant продолжить единственного совместимого upstream ancestor. Вложенные `loop_group` остаются запрещены в `v1alpha1`.

### `subworkflow`

Подключает отдельный `takt/v1alpha1 Workflow` и компилирует его в тот же DAG до запуска:

```yaml
- id: implementation
  provider: opencode
  model: main
  context: shared
  subworkflow:
    path: workflows/implementation.yaml
    inputs:
      plan: $ARGUMENTS
    output_node: result
```

Путь вычисляется относительно содержащего workflow. В подключённом файле
значения доступны как `$INPUTS.<name>`. Неразрешённая обязательная ссылка
является ошибкой загрузки; `$INPUTS.<name>?` и `$INPUTS.<name>:-default`
разрешаются явно. Если terminal-узел один, `output_node` выводится
автоматически; при нескольких terminal-узлах поле обязательно.

Публичный ID контейнера сохраняется для `depends_on` и `$<id>.output`. CLI
показывает только публичные узлы. Внутренние namespaced ID с `__` сохраняются
в `state.json` для точного resume и проверки определения. Approval внутри
подключённого workflow отображается и принимается через публичный ID контейнера.

Локальная Markdown-команда сначала ищется в `commands/` рядом с подключённым workflow, затем в родительских каталогах до корня композиции. Поэтому workflow из `profiles/code/workflows/` использует команды из `profiles/code/commands/`. Содержимое встроенной команды входит в workflow fingerprint.

`provider`, `model` и `context` на контейнере задают defaults вызова. Приоритет:
явное поле дочернего узла → контейнер → defaults дочернего workflow → defaults
родительского workflow. Положительный `attempts.max`, непустые `timeout`, hooks
и `native_hooks`, а также `allow_failure: true` задаются внутри подключённого
workflow. Нулевые и пустые значения этих полей трактуются так же, как
отсутствие поля; схема повторяет эту семантику кода.

Рекурсивная ссылка отклоняется с цепочкой файлов. Максимальная глубина развёртывания — 16 одновременно активных workflow; превышение возвращает `subworkflow expansion exceeds depth 16`.

### `workflow` — governed child Run

Запускает подключённый workflow как отдельный Run со своими state, events, artifacts, fingerprints, output и usage:

```yaml
- id: feature
  workflow:
    path: workflows/feature-development.yaml
    input: $ARGUMENTS
    output_node: summary
    isolation: inherit
    policy:
      denied_tools: [edit, write]
      sandbox:
        filesystem: read_only
```

`path` вычисляется относительно содержащего workflow. `input` проходит обычный renderer. Если `output_node` не задан, в дочернем определении должен быть ровно один terminal-узел. В отличие от `subworkflow`, дочерние узлы не встраиваются в DAG родителя.

Ребёнок хранит `parent_run_id` и `parent_node_id`; родитель — список `child_run_ids`, а узел — текущий `child_run_id` и историю попыток. Failure/cancellation ребёнка определяет результат родительского узла. Retry после failed/cancelled child создаёт новый child Run; уже `completed` child переиспользуется, поэтому повтор post-child hook/validation не повторяет успешную mutating работу.

Режимы `isolation`:

- пусто — собственная `worktree`-политика ребёнка;
- `inherit` — execution workspace родителя без отдельного worktree;
- `worktree` — принудительно отдельный managed worktree;
- `none` — control workspace без worktree.

Approval ребёнка переводит родителя в `waiting` с `kind: child_run`. `takt answer` можно вызвать по корневому Run ID и публичному ID родительского `workflow`-узла; CLI продолжит фактический approval и затем всю parent chain. `takt cancel` распространяет отмену по дереву. Статические child definitions входят в fingerprint родителя; рекурсия отклоняется, глубина ограничена 16.

Для динамического набора детей используется `workflow.fan_out`:

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
      allow_empty: false
      allow_duplicates: false
```

`items_from` должен указывать на JSON-массив в структурированном output upstream-узла. `max_parallel` по умолчанию равен 1 и ограничен 64. `join` принимает `all_success`, `all_done` или `one_success`. Каждый элемент получает отдельный child Run и устойчивую запись в состоянии; completed-дети переиспользуются при resume, а изменение массива внутри попытки отклоняется. В `input` доступны `$FANOUT.item`, `$FANOUT.index`, `$FANOUT.total` и алиас из `as`. Дубли канонических элементов отклоняются по умолчанию; `allow_duplicates: true` является явным разрешением двойного запуска. Output родительского узла — упорядоченный JSON-массив статусов, outputs, usage, Run ID и при наличии `cancel_reason`. `one_success` отменяет уже ненужных siblings после первого success; `all_success` прекращает оставшуюся работу после первого failure-like результата; `all_done` ждёт всех children. Такая внутренняя отмена имеет `cancel_reason: fanout_result_decided` и отличается от operator cancellation.

### `foreach`

Выполняет один subworkflow для элементов из workflow или отдельного YAML/JSON-файла. По умолчанию итерации последовательны; `parallel: true` делает их независимыми узлами одной DAG-волны:

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

Нужно задать ровно один источник: `items` или `items_from.path`. Путь вычисляется относительно содержащего workflow; файл должен содержать непустой массив верхнего уровня. Его исходные байты входят в fingerprint определения, поэтому изменение списка блокирует resume ранее начатого Run.

Поддерживаются scalar и inline JSON objects. Для объекта доступны `$INPUTS.check`
как JSON и `$INPUTS.check.<field>` для полей; `$INPUTS.index` содержит индекс с
нуля. При `parallel: false` каждая итерация зависит от предыдущей; при
`parallel: true` все итерации зависят от общего gate и могут выполняться
конкурентно. Публичный output всегда является JSON-массивом результатов в
исходном порядке; JSON-результаты сохраняют тип, остальные результаты становятся
строками.

Runtime читает только явный массив и не преобразует Markdown-план в task AST.

## 7. Зависимости, ошибки и итог Run

Узел начинает выполнение после terminal-состояния зависимостей.

Поддерживаемые `trigger_rule`:

- `all_success` — все зависимости `completed`;
- `all_done` — любые terminal-состояния зависимостей;
- `none_failed_min_one_success` — нет failure-like зависимостей и есть хотя бы одна `completed`;
- `one_success` — после завершения всех зависимостей запускает узел, если хотя бы одна зависимость `completed`, включая соединение взаимоисключающих ветвей.

После failed/errored/timed_out node scheduler продолжает DAG, чтобы выполнить `all_done`. `always_run` является явной cleanup-семантикой `all_done`; его нельзя совмещать с `when` или другим `trigger_rule`. Итоговый статус Run вычисляется после завершения доступного графа.

Статусы Run:

- `running`;
- `pausing` — оператор запросил безопасную паузу, активные попытки доходят до границы узла;
- `paused`;
- `waiting`;
- `completed`;
- `failed`;
- `cancelled`;
- `abandoned` — оператор завершил обслуживание Run с сохранением истории.

Terminal-состояния Run: `completed|failed|cancelled|abandoned`.

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
when: $analyze.exit_code == 0
when: $classify.output == "feature"
when: $classify.output.workflow == "fix-github-issue"
when: $INPUTS.input != "dry-run"
when: $classify.output == "feature" && $validate.output == "ready"
```

`when` является намеренно малым gate language, а не общим языком выражений. В `takt/v1alpha1` разрешены только `==`, `!=`, `&&` и `||`; `&&` имеет больший приоритет, чем `||`. Левый операнд — `$<id>.<path>` или `$INPUTS.<name>`, правый — литерал. Скобки, функции, арифметика, regex и операторы порядка не входят в контракт. Более сложное условие вычисляется отдельным `script`/`command`/`prompt` узлом и передаётся дальше через structured output. Loader проверяет синтаксис `when` до создания Run, runtime использует ту же реализацию `internal/whenexpr`.

Это часть конституции языка workflow: **YAML координирует, код вычисляет, агент принимает решения**. Новые операторы не добавляются по одному; при доказанной потребности в полноценном expression language он должен быть принят целиком как отдельный versioned contract change.

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

Stdout и stderr неуспешного hook добавляются в `$FEEDBACK` следующей попытки.

## 9. YAML input

YAML syntax разбирается upstream-библиотекой `go.yaml.in/yaml/v3`. Takt не поддерживает собственную грамматику или отдельный YAML subset: карты, списки, quoted/plain scalars, block scalars, anchors/aliases и другие синтаксические возможности определяются выбранной YAML-библиотекой.

Поверх parser Takt применяет свой публичный contract layer:

- канонические имена полей берутся из существующих `json` tags;
- неизвестные поля отклоняются с диагностикой и подсказкой ближайшего имени;
- YAML и JSON проходят через одну нормализованную JSON-shaped модель до декодирования в структуры Takt;
- semantic validation Workflow/Config/Profile остаётся в соответствующих валидаторах Takt.

Для многострочных `prompt` и `bash` рекомендуется обычный YAML block scalar `|`.

## 10. Переменные

Единый `internal/flowref` parser обслуживает все поверхности:

- `NonShell`: `$ARGUMENTS`, `$FEEDBACK`, `$INPUTS.<name>`,
  `$<id>.output[.<field>]`, `$<id>.status`, `$<id>.exit_code`,
  `$LOOP_PREV.<id>.output[.<field>]`, `$<approval>.output`;
- `Shell`: те же ссылки с context-aware quoting; `$ARGUMENTS`, `$FEEDBACK`,
  `$ARTIFACTS_DIR`, `$BASE_BRANCH` и `$TAKT_WORKSPACE` передаются через env;
- `ScriptArg`/`ScriptEnv`: значения передаются как argv/env без shell
  interpolation;
- `When`: только левая ссылка из `nodes`/`inputs` и операторы `==`, `!=`, `&&`,
  `||`.

`$path` обязателен, `$path?` допускает отсутствие, `$path:-default` задаёт
fallback. `$FANOUT.item`, `$FANOUT.index`, `$FANOUT.total` действуют внутри
fan-out; `$INPUTS.<name>` — внутри подключённого subworkflow. В non-shell
поверхностях `$$` становится literal `$`; в shell-сценариях `$$`, `$?`, `$1`,
`$((...))` и `$(...)` сохраняются как native shell syntax. Legacy `${...}`,
`$USER_MESSAGE`, reserved node IDs и positional artifact indexes отклоняются.
Неразрешённая обязательная ссылка возвращает authoring/runtime error, а не
литеральный token.

В Shell surface Takt reference внутри double quotes отклоняется; в
single-quoted сегменте значение экранируется внутри существующей кавычки.
Нативные `$PATH`, `${PATH}`, `$?`, `$$`, `$((...))` и `$(...)` сохраняются.
`$BASE_BRANCH` разрешается только из durable worktree base и при его отсутствии
fail-closed до запуска bash. `$TAKT_WORKSPACE` указывает на текущий
execution workspace, а при его отсутствии — на control workspace.

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
- результаты последней loop iteration;
- parent/child links, run output, aggregate usage и durable cancellation state.

Каждый commit состояния и события получает одну revision. При несовпадении ревизий `Load` возвращает `store_inconsistent`.

`answer` и `resume` используют lock Run и блокируются при изменении определений после старта.

## 12. Профили и именованные workflow

`Profile` задаёт default workflow, config и необязательную карту именованных процессов:

```yaml
apiVersion: takt/v1alpha1
kind: Profile
metadata:
  name: code
workflow: workflow.yaml
workflows:
  assist: workflows/assist.yaml
  piv-loop: workflows/piv-loop.yaml
config: ../../config.yaml
```

Селектор `code` запускает default workflow, например умный роутер. Селектор `code:piv-loop` выбирает именованный файл без роутинга. Имена не могут содержать `:`; пути вычисляются относительно `profile.yaml`.

## 13. JSON CLI

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
takt run <workflow> --config <config> --workspace <dir> --input <file-or-text> [--model-preset <name>]
takt answer <run-id> <node-id> --workspace <dir> --value <text>
takt resume <run-id> --workspace <dir>
takt status <run-id> --workspace <dir>
takt children <run-id> --workspace <dir>
takt cancel <run-id> --workspace <dir> [--reason <text>]
takt command run <name> --config <config> --workspace <dir> --input <text> [--model-preset <name>]
takt workflow list <profile> --workspace <dir>
takt workflow describe <profile[:workflow]> --workspace <dir>
takt worktree list --workspace <dir>
takt worktree remove <run-id> --workspace <dir> [--force]
takt worktree prune --workspace <dir>
takt eval run <workflow> --config <config> --cases <dir> --workspace-template <dir> --output <dir> [--model-preset <name>] [--strategy-id <id>] [--benchmark-id <id>] [--quality-node <id>] [--generation-node <id>] [--validator-path <path>]
takt eval report <evaluation-output-dir>
takt eval stats <evaluation-output-dir> [--json]
takt eval status <evaluation-output-dir> [--json]
takt eval inspect <evaluation-output-dir> [--case ID] [--repeat N] [--json]
takt eval analyze <evaluation-output-dir> [--case <case-id>] [--repeat N] [--config <analyzer-config>] [--model-preset <name>] [--language en|ru] [--trace] [--json]
takt eval benchmark <matrix.yaml> [--output <dir>] [--repeat N] [--replace]
takt eval compare <baseline-output-dir> <candidate-output-dir>
```

### Production flow evaluation

`model_presets` is a shared Config feature, not an evaluation-only format. Each
preset is a non-empty map of arbitrary aliases to atomic `provider/model-id`
values, split at the first `/`. `model_preset` selects the preset for that
Config; CLI `--model-preset` temporarily overrides it. `models` and
`model_presets` are mutually exclusive. Missing workflow aliases and malformed
references fail before Run creation. Only effective models enter the Config
fingerprint; editing an unselected preset does not change it. Repeated
`--model alias=provider/model` overrides are accepted for any alias.

`takt eval flow <suite.yaml> [--case ID] [--repeat N] [--output DIR]
[--assistant-idle-timeout DURATION] [--keep-workspaces] [--trace] [--json]` runs sequential isolated production-shaped cases.
The strict `takt-flow-evaluation/v1alpha1` suite declares `workflow`, `config`,
`cases.directory`, a validator command/path/timeout/output limit, and gates.
Each repeat persists `cases/<case>/repeat-<NNN>/run.json`,
`activity.json`, `validation-request.json`, `validation-result.json`, and
`artifacts/manifest.json`;
`report.json` remains the canonical `takt-evaluation/v1alpha1` output. Validator
stdout alone is decoded as `takt-validation/v1alpha1`; agent text is not proof.
Gate failure returns non-zero only after report persistence.
От запуска набора до финализации Takt атомарно заменяет
`<evaluation-output-dir>/progress.json` по контракту
`takt-flow-evaluation-progress/v1alpha1`. Команда `takt eval status <dir>` читает
этот снимок, не запуская процесс, не вызывая ассистента и не подключаясь к
Pi. Фазы имеют точные значения `prepare`, `validator_preflight`, `workflow`,
`validator`, `evidence`, `cleanup` и `finalized`. Токены и стоимость выполнения
включают только завершённые исполнения, уже сохранённые в состоянии Run.
Принудительно остановленный процесс оценки может оставить `status: running`;
устаревший снимок определяется по `updated_at`. Необязательный массив
`runtime.assistant_activity[]` содержит
для каждого активного ассистента узел и попытку, наблюдаемое клиентом состояние
провайдера, время начала состояния, порядковый номер вызова модели, сведения о
повторе и задержке, а также последнюю отредактированную ошибку провайдера.
Финальный снимок остаётся рядом с `report.json`.
`report.json` is first checkpointed after a case reaches validator/evidence; it
is not a live heartbeat. Before that checkpoint, `takt eval inspect <dir>`
synthesizes the current case and running nodes from `progress.json` with
`reported_cause.confidence=UNAVAILABLE`. `takt eval analyze <dir>` requires
completed case evidence and fails immediately with `evaluation is still
running` without loading analyzer configuration or contacting a model.
`--trace` writes elapsed suite stages, durable root Run/node events and terminal
child Run/node statuses to stderr while stdout remains the final JSON result.
Human trace lines use `SCOPE | EVENT | DETAILS`: `EVAL`, `CASE <id>#<repeat>`,
`REPORT`, or `RUN <short-id> · NODE <id>#<attempt>`. The full root Run ID is
printed once at `accepted`; child snapshots also retain their full ID. Model and
Session ID are announced on session start/resume or the first event that reveals
a fresh Session ID, then omitted from repeated tool/message lines. Report writes
are labelled `checkpoint phase=validation`, `checkpoint phase=cleanup`, or
`finalized` rather than emitted as indistinguishable duplicates.
Встроенный адаптер Pi в реальном времени показывает начало и завершение
инструментов, ограниченные превью сообщений ассистента и наблюдения жизненного
цикла провайдера. Повторяющиеся обновления потока остаются временными, а первое
событие потока, завершение и повторы сохраняются как отредактированные
свидетельства.
Если сохраняемого перехода нет 30 секунд, `node.active` показывает `run`,
`node`, `attempt`, прошедшее время `idle`, действующий `idle_limit`, последний
измеренный контекст запроса модели, `last_activity` и ожидаемую адаптером
границу. Контекст выводится как `context=<tokens>t`, только если сообщение
ассистента содержит число входных токенов отдельного запроса; иначе явно
указывается `context=unknown`. Это не накопленная статистика попытки и не
максимальное окно контекста модели. События Pi `message_update` и
`tool_execution_update`
сбрасывают счётчик неактивности, но не печатаются для каждого токена и не
сохраняются как долговременные события ассистента.
`--assistant-idle-timeout` defaults to `5m` and supplies an eval-only fallback
for assistant nodes that omit `idle_timeout`; explicit node values win. Valid
assistant tool/message events reset the timer, and expiry is persisted as
`node.timed_out` with `error_code=timed_out` before validation and report writing.
Make targets `eval-feature`, `eval-feature-smoke`, `eval-review` и
`eval-architect` передают в этот флаг переменную `EVAL_IDLE_TIMEOUT` (по
умолчанию `5m`),
например: `EVAL_IDLE_TIMEOUT=10m make eval-feature`. Это настройка только
evaluation и не изменяет production workflow.

`takt eval flow init <workflow-selector> --output <directory>` creates only a
suite skeleton and one example case. It never creates a validator or executable
setup: add `config.yaml`, implement `./validator`, and replace the example case.

`takt eval analyze` is a read-only advisory pass over a saved flow evaluation.
The selected analyzer Config must materialize the dedicated `takt_analyze` model
alias; no other alias is used as a fallback. `--language en|ru` controls the
language of human-readable advisory values and defaults to `en`; JSON keys and
enum values remain stable. `failure_mode` is also language-independent and must
be a lowercase snake_case machine code matching `^[a-z][a-z0-9_]*$`; localized
explanations belong in `root_cause`, `causal_mechanism`, and `prevention`. The
selected language is persisted in the analysis report and manifest. Without `--case`, every saved run
whose outcome is not `true_accept` is selected. `--repeat` requires `--case`.
The command creates a UTC timestamped `analyses/<timestamp>/` directory with a
redacted manifest, per-case evidence manifest and `analysis.json`, and leaves the
source `report.json` byte-for-byte unchanged. An empty selection succeeds with
`status=no_cases`; provider, protocol and persistence failures are retained in a
saved `status=failed` report and never change the deterministic verdict. The
case report stores the redacted rendered analyzer prompt and its SHA-256
fingerprint; citations are resolved against the bounded evidence manifest. The
generated `evidence-manifest.json` itself is a checked citation target (for
example `evidence-manifest.json#/deterministic_verdict/outcome`) in addition to
the files listed under `files`; its JSON pointer is validated against the
saved manifest before the advisory result is accepted. A citation may include
the manifest's `evidence_root/` prefix (for example `evidence/run.json`) and is
normalized only when its suffix is listed in `files`. Citation validation also
normalizes common equivalent forms: `#/pointer` in structured entries,
`path:line-range` in causal strings, and zero-based `/N` for a text file's
line index. Canonical output remains `/pointer`, `line:N`, and
`path#line:N`. If the analyzer returns
malformed JSON or violates the advisory contract, the bounded model output is
saved as redacted `raw_output_path` beside that case's `analysis.json` when
available. An adapter-provided relative `session_path` is resolved against the
analysis execution workspace before cleanup; paths that escape it or traverse
a symlink are recorded as unavailable. A completed advisory analysis must add
`causal_mechanism`, bounded `failure_point` (`assistant_decision`,
`workflow_control`, `validator`, `infrastructure`, or `unknown`) and one
concrete `prevention` to the deterministic cause. At least one checked citation
must come from runtime, assistant, artifact, source, diff, or SCM evidence;
validator request/result/stderr and the evidence manifest alone are
insufficient. These advisory fields explain how the persisted failure arose
but cannot replace or modify the deterministic verdict.

Все команды поддерживают `--json`; `run`, `answer`, `resume`, `status`, `children`, `artifacts`, `cancel`, `command run` и `eval` используют JSON по умолчанию.

`eval run` выполняет preflight до создания output: нормализованные `case_id` должны быть уникальны, а `workspace-template` и `output` не могут совпадать или быть вложены друг в друга, включая пути через символические ссылки. До запуска вычисляются fingerprints workflow, config, Markdown-команд, упорядоченного набора заданий, копируемого workspace template и указанного валидатора.

`report.json` использует `takt-evaluation/v1alpha1` и сохраняет strategy/benchmark identity, версию Takt и Go-окружение, assistant и его версию, requested model, фактический Pi `responseModel`, attempts, duration, usage, approval answers, statuses, resume, feedback, ошибки узлов и диагностический вывод. В flow evaluation optional `nodes.<id>.duration_ms` измеряется по durable events от первого `node.started` до terminal event. Это wall-clock длительность всего узла, включая tool calls, retry backoff и ожидания, а не чистое provider inference time; старые отчёты без событий сохраняют поле недоступным.

`takt eval stats <evaluation-output-dir>` загружает существующий suite report и
не запускает workflow или model calls. Human-readable output используется по
умолчанию; `--json` возвращает `takt-evaluation-stats/v1alpha1` согласно
`schemas/evaluation-stats.schema.json`: identity, outcomes, node attempts,
assistant executions, attempts/retries, tokens, duration/time-to-valid, cost,
diagnostics, usage identities, case rows и wall-clock таблицу assistant steps.
Отдельный `assistant_sessions` сохраняет для каждой фактической assistant
execution полный Session ID, workflow/provider attempt и признак resume; human
output показывает их в секции `ASSISTANT SESSIONS`. Takt не строит URL: adapter
contract предоставляет только opaque Session ID.

Для неуспешных cases stats показывает приоритетную причину с явным источником:
flow validator, ошибка запуска validator, quality diagnostic, root runtime или
первый стабильный по ID node error. `takt eval inspect <evaluation-output-dir>
[--case ID] [--repeat N]` даёт детерминированный read-only разбор той же причины,
non-completed узлов и сохранённых `run.json`, validation, diff/source,
`repository.bundle`, artifacts и SCM calls. `activity.json` содержит только
redacted нормализованные `assistant.tool.started` с tool input; assistant
messages и tool output туда не копируются. Секция `CAUSAL CHAIN` коррелирует
только сохранённые факты: terminal assistant reason/usage и tool calls из
redacted `run.json`, пустой assistant result, deterministic validation failure
и skipped downstream nodes. Например, `stopReason: length` связывается с
достигнутым output-token limit и отсутствием direct write/edit calls.
Наблюдение маркируется отдельно от reported cause и получает
`CONFIRMED`, `INFERRED` или `UNAVAILABLE`. `inspect` не запускает и не
возобновляет workflow, не обращается к assistant/provider и не меняет verdict
валидатора. LLM-анализ не является частью этой команды.

Каждая фактическая попытка действия сохраняется в `nodes.<id>.executions`. Summary группирует tokens/cost по `usage_by_execution_identity`; при смене assistant, его версии, requested или resolved model узел получает `mixed_execution_identity: true`.

При заданном `--quality-node` Takt декодирует доступный строгий `takt-validation/v1alpha1` только из stdout узла и независимо от exit code и terminal status. Stderr сохраняется отдельно и входит в объединённый диагностический output, но не участвует в декодировании envelope. `score`, `checks` и diagnostics сохраняются и участвуют в предметных агрегатах даже для `valid: false` с ненулевым exit code. Успех определяется только сочетанием `quality_node_status: completed` и `quality.valid: true`; результат из failed/errored/timed_out/cancelled/skipped/blocked узла не повышает success rate. Malformed envelope при любом статусе является ошибкой измерительного контура. Runner агрегирует `success_at_1`, итоговую долю корректных результатов, среднюю оценку, попытки до успеха, стоимость, `amortized_end_to_end_ms_per_valid`, настоящий `average_time_to_valid_ms`, failed-execution cost, retry и diagnostics по severity/code/fingerprint. При repeat > 1 также вычисляется стабильность каждого case.


`EvaluationMatrix` (`takt/evaluation/v1alpha1`) задаёт общий benchmark, обязательную baseline strategy, несколько workflow/config стратегий, repeat и regression gates. `takt eval benchmark` сохраняет `benchmark.json` (`takt-evaluation-matrix/v1alpha1`), а `takt eval compare` требует одинаковый benchmark fingerprint и строит парные исходы по `case_id + repeat`. Human compare использует короткие имена `A`/`B`, показывает output directories, presets/models, явные `BETTER|WORSE|SAME|NOT MEASURED|NOT COMPARABLE` для correctness/reliability/efficiency и каждого показателя, а также переход каждого case. Общий verdict сначала учитывает число корректных результатов, затем reliability и только при равном качестве — resources. Поэтому экономия attempts не маскирует потерю valid results. Для flow-метрик больше valid/completion считается лучше, меньше false accepts/false rejects/infrastructure/validator errors — лучше; для tokens/time/attempts/cost лучше меньше. Проценты всегда сопровождаются счётчиком и знаменателем. Delta остаётся `B-A`. `CaseManifest` фиксирует labels корпуса и входит в benchmark fingerprint. Gate failure возвращает non-zero после сохранения полного отчёта.

Измеренные нулевые доли сериализуются как `0`. Метрики, которые нельзя вычислить, например average score без score или cost per valid без корректных результатов, сериализуются как `null`. Общий benchmark fingerprint включает ID, версию и fingerprint валидатора. Workflow и предметный валидатор остаются источником критерия качества; Takt не интерпретирует семантику Route DSL.

## 14. Ограничения

- параллельная волна не включает узлы с portable hooks или `attempts.max > 1`;
- вложенный `loop_group` внутри `loop_group` запрещён;
- `native_hooks` передаются адаптеру, но не исполняются runtime;
- несколько `workflow`-узлов пока не выполняются одной параллельной волной;
- локальный OS sandbox применяется к запускаемым Takt `bash/script`; `command/prompt` filesystem/network policy остаётся assistant-enforced, а полноценная untrusted/multi-user boundary, server, Web UI и БД — proposal вне локального режима;
- stale lock требует ручного удаления после аварийного завершения процесса;
- специализированные Pi и OpenCode adapters реализованы;
- `takt-assistant/v1alpha1` реализован для универсального `process`; специализированный `pi` использует официальный Pi RPC JSONL, а `opencode` — официальный `run --format json` event stream; потоковые события пока не публикуются в EventSink.


## Managed worktree policy

```yaml
worktree:
  enabled: true
  base: HEAD
  branch_prefix: takt
  cleanup: on_success
  allow_dirty: false
```

State and artifacts remain in the control workspace. Node execution moves into the worktree. `cleanup` is `on_success` or `manual`. Automatic cleanup applies only to a clean successful worktree; an unchanged branch is deleted, while a branch with commits and all states that may contain evidence or changes are retained. `--no-worktree`, `--keep-worktree`, `--worktree-base`, and `--allow-dirty-worktree` override policy and are persisted for resume.

## Уточнение public adapter contracts v0.1.49

Process `takt-assistant/v1alpha2` является transport protocol, а не неявным набором security capabilities. Реальные capabilities задаются Config и подтверждаются первой stream declaration. Runtime отклоняет configured capability, отсутствующую в declaration, event вне declared `event_types` и `tool.request` без `tool_control`.

Public `sdk/domainadapter.InvokeRequest`/`ReconcileRequest` включают необязательный `workspace` — execution workspace node. Process transport использует его как cwd. Provider-specific repository identity остаётся в adapter input/config; multi-repo `change.create` передаёт `repository_workspace` с точным child execution worktree.

## Structured Task Sources — v0.1.50

`Config.task_sources` declares trusted external ingress adapters. Process adapters use `takt-task-source/v1alpha1` and return a normalized Task with `source.adapter`, `kind`, `reference`, immutable `revision` and optional URL.

`task start` / `takt.task.start` accept exactly one input mode:

```text
goal
or
source + source_ref
```

Source resolution happens before the existing Task Router. Router, Dynamic Planner and Replanner receive the same structured `task_source`; ordinary workflow input remains the compatible compiled GoalText. Resume/replan does not re-fetch the source. Task Sources are ingress and are distinct from `adapter` nodes / Domain Adapter side effects.


## Human-reviewed Learning Loop — v0.1.51

`takt learn` operates on durable local Run history and does not add a workflow node or second runtime. `scan` groups only stable identifiers already persisted by Takt: diagnostic fingerprints across distinct Runs and completed workflow fingerprints. `min-runs` is at least `2`.

`propose` snapshots a `skill` or one-block `BlockPackage` under `.takt/learning/proposals/<id>/candidate`, records supporting Run IDs, expected benefit and SHA-256, and creates `takt-learning/v1alpha1 LearningProposal` in `pending_review`. Skill frontmatter `name` must match the proposal candidate name. Candidate trees reject symlinks/non-regular files.

`review` requires explicit `accept|reject` plus a non-empty human rationale. An accepted proposal still cannot be staged until `learn evaluate` snapshots a passing `takt-evaluation-matrix/v1alpha1` or `takt-task-evaluation-matrix/v1alpha1` report with `matrix_fingerprint`, `benchmark_id` and at least one passing regression gate.

`stage` re-hashes the candidate and fails if the reviewed snapshot changed. A successful stage copies only to `.takt/learning/ready/<id>`; it never updates package locks, profile package lists, global/corporate scopes or assistant skill configuration. Activation remains an explicit operation outside the learning loop. Machine-readable contract: `schemas/learning-proposal.schema.json`.
