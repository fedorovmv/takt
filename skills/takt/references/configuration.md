# Конфигурация Takt

## Минимальный config

```yaml
apiVersion: takt/v1alpha1
kind: Config
default_assistant: pi

models:
  main:
    provider: aihub
    id: Qwen/Qwen3.6-27B
    params:
      reasoning_effort: high

assistants:
  pi:
    type: pi
    binary: pi
    session_dir: .takt/pi-sessions
    project_trust: deny
    max_output_bytes: 10485760
```

`default_assistant` разрешает логическое имя `coding-agent` в профилях. `models` содержит aliases, на которые ссылаются workflow и Markdown-команды. `provider` и `id` должны поддерживаться выбранным assistant.

Для повторяемого выбора provider/model можно объявить общий preset:

```yaml
model_preset: qwen36
model_presets:
  qwen36:
    implementation: aihub/Qwen/Qwen3.6-27B
    review: aihub/Qwen/Qwen3.6-27B
    routing: aihub/Qwen/Qwen3.6-27B
```

Preset содержит произвольные aliases и атомарные значения `provider/model-id`.
`models` и `model_presets` нельзя смешивать. `takt run ... --model-preset NAME`
и eval используют один resolver; без CLI выбора применяется `model_preset`.

## Несколько моделей

```yaml
models:
  fast:
    provider: aihub
    id: Qwen/Qwen3.6-27B
    params:
      reasoning_effort: medium

  deep:
    provider: aihub
    id: Qwen/Qwen3.6-27B
    params:
      reasoning_effort: high
```

Узел выбирает alias:

```yaml
- id: draft
  prompt: Подготовь решение для $ARGUMENTS
  model: fast

- id: repair
  depends_on: [draft]
  prompt: Исправь результат после проверки
  model: deep
```

Takt не переключает модель автоматически между attempts одного узла. Для управляемого перехода используй разные узлы.

## Assistant `pi`

Поддерживаемые поля:

```yaml
assistants:
  pi:
    type: pi
    binary: pi
    args: [--offline]
    session_dir: .takt/pi-sessions
    project_trust: deny
    settings:
      httpIdleTimeoutMs: 300000
    env:
      EXAMPLE: value
    capabilities: [custom_extension]
    max_output_bytes: 10485760
```

- `binary` — путь к Pi, по умолчанию `pi`;
- `args` — дополнительные нерезервированные параметры;
- `session_dir` — каталог сессий;
- `project_trust` — `default`, `approve` или `deny`;
- `settings` — нативный JSON-объект Pi, который контур оценки переносит в
  изолированный `.pi/settings.json`; оценка принудительно отключает вложенные
  повторы агента и SDK провайдера;
- `env` — переменные окружения процесса;
- `max_output_bytes` — общий лимит RPC stdout/stderr.

Не добавляй в `args` флаги, которыми управляет adapter: `--mode`, `--provider`, `--model`, `--thinking`, `--session`, trust и session directory.

## Assistant `process`

```yaml
assistants:
  external:
    type: process
    argv:
      - ./tools/assistant-adapter
      - --model
      - "{{model.id}}"
    protocol: takt-assistant/v1alpha2
    max_output_bytes: 10485760
```

Шаблоны `argv`:

- `{{prompt}}`;
- `{{run.id}}`, `{{node.id}}`, `{{attempt}}`;
- `{{model.name}}`, `{{model.id}}`, `{{model.provider}}`, `{{model.params}}`;
- `{{workspace}}`;
- `{{session.mode}}`, `{{session.id}}`.

`hook.on_failure.session` accepts only `fresh` or `resume` and is valid only when
`action` is `retry`.

Если protocol не задан и `{{prompt}}` отсутствует, prompt передаётся через stdin. `v1alpha2` является нейтральной границей `SessionAdapter` для Codex, Oh My Pi, Qwen CLI и других coding-agent wrappers; имена примеров не означают, что wrappers входят в поставку.

## Логический coding-agent

Встроенный профиль `code` ссылается на `provider: coding-agent`, а конкретный
исполнитель выбирается один раз через Config binding:

```yaml
default_assistant: qwen
assistants:
  qwen:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: [qwen-takt-adapter]
```

Порядок разрешения: явный assistant с именем `coding-agent`, затем `default_assistant`, затем совместимый legacy fallback `opencode`, затем единственный объявленный assistant. Неоднозначная конфигурация отклоняется. Workflow не меняется при переходе между Pi, OpenCode, Codex, Oh My Pi, Qwen CLI и другим совместимым адаптером.

Пример нескольких внешних адаптеров находится в `examples/agent-session-adapters/`. Takt не зависит от Kiro CLI и не включает готовые wrappers для каждого стороннего продукта.


## Role Contract в trusted BlockPackage

RoleDefinition живёт в пакете Takt, а не в глобальном каталоге agents кодинг-хоста. Роль задаёт `model_profile`, `session`, bounded `context`, path scope и поддерживаемую adapter policy. Блок ссылается на роль и может объявить structured checks:

```yaml
roles:
  verifier:
    model_profile: review
    session: fresh
    context:
      include: [prior_results, signals, scope]
      max_chars: 42000
    paths:
      allowed: ["**"]
      forbidden: [".git/**", ".takt/**"]
    policy:
      sandbox:
        filesystem: read_only

blocks:
  review:
    role: verifier
    workflow: dynamic-review.yaml
    output_paths: [summary, approved]
    checks:
      - name: independent-review
        path: approved
        level: required
        reaction: repair
```

`expected` — диагностическая область, `allowed` — допустимая область результата, `protected` требует сохранения усиленной проверки, `forbidden` блокирует результат. Для mutating-role не объявляй вымышленный write sandbox: текущий `sandbox.filesystem` гарантирует только `read_only`; ограниченная запись по путям проверяется Takt по структурированному `changed_files` и будет усилена отдельным OS sandbox в будущем.

## Приоритет настроек

Для `command` и `prompt`:

1. `node.provider` / `node.model`;
2. `provider` / `model` во frontmatter команды;
3. `workflow.provider` / `workflow.model`.

`context: fresh` задаётся в узле; значение по умолчанию — `fresh`. Для
продолжения совместимой upstream-сессии используй `context: shared`.

## Assistant opencode

```yaml
assistants:
  opencode:
    type: opencode
    binary: opencode
    agent: build
    auto_approve: false
    env:
      EXAMPLE: value
    max_output_bytes: 10485760
```

- `binary` — путь к OpenCode, по умолчанию `opencode`;
- `args` — дополнительные нерезервированные параметры `opencode run`;
- `agent` — имя OpenCode agent, передаваемое через `--agent`;
- `auto_approve` — включает `--auto`; используй только в доверенном workspace;
- `env` — переменные окружения OpenCode;
- `max_output_bytes` — общий лимит JSON stdout и диагностического stderr.

Takt запускает `opencode run --format json --dir <workspace> --model <provider>/<id>`.
Параметр модели `variant` передаётся через `--variant`; если `variant` отсутствует, используется строковый `reasoning_effort`.
При `context: shared` Takt передаёт сохранённый Session ID через `--session` и
проверяет, что OpenCode вернул тот же идентификатор. Межитерационный resume
внутри `loop_group` управляется `fresh_context: false`.

Не добавляй в `args` управляемые adapter флаги: `run`, `--format`, `--model`, `--agent`, `--session`, `--dir`, `--variant`, `--auto` и их короткие варианты.
JSON events читаются из stdout, stderr остаётся диагностическим. Usage одной попытки — сумма событий `step_finish`.

## Возможности adapter

Стандартные names: `tool_policy`, `skills`, `mcp`, `sandbox_filesystem`, `sandbox_network`. Pi/OpenCode публикуют встроенные возможности автоматически и не могут через config приписать себе неподдерживаемую зарезервированную capability. `process` объявляет возможности явно, потому что получает policy через протокол и `TAKT_POLICY_JSON`. Дополнительные пользовательские names применяются вместе с `requires`.


## Skills в Pi и OpenCode

Pi принимает только существующие path skills: Takt проверяет путь до запуска и передаёт его через `--skill`. Именованный skill без файла для Pi завершится ошибкой конфигурации. OpenCode поддерживает path skills и именованные skills: содержимое path skill добавляется в prompt, именованный skill ограничивается через permissions.

При `sandbox.filesystem: read_only` OpenCode принудительно запрещает `write`, `edit`, `bash` и `task`, даже если инструмент указан в `allowed_tools`.

## Доменные adapters

SCM, tracker и CI подключаются отдельно от coding assistant:

```yaml
adapters:
  scm:
    domain: scm
    transport: mcp
    argv: [corp-scm]
    operations:
      change.create: corp_change_create
    reconcile_operations:
      change.create: corp_change_reconcile
```

`domain` — `scm|tracker|ci`, `transport` — `process|mcp`. Workflow использует только нейтральное `adapter.name` и `adapter.operation`. Для process transport действует `takt-domain-adapter/v1alpha1`; MCP transport получает capabilities через `tools/list`. Проверяй подключение через `takt adapter doctor <name>` до запуска mutating workflow.


## Task Sources

```yaml
task_sources:
  github:
    transport: process
    argv: [takt-github-task-source]
    env:
      GH_TOKEN: secret://GH_TOKEN
    timeout: 30s
```

Source adapter разрешает внешний объект до Router и возвращает `takt-task-source/v1alpha1`. `secret://` разрешается непосредственно перед запуском процесса.
