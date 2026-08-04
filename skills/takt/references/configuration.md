# Конфигурация Takt

## Минимальный config

```yaml
apiVersion: takt/v1alpha1
kind: Config

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

`models` содержит aliases, на которые ссылаются workflow и Markdown-команды. `provider` и `id` должны поддерживаться выбранным assistant.

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
  prompt: Подготовь решение для ${input}
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
    env:
      EXAMPLE: value
    capabilities: [session_resume, skills]
    max_output_bytes: 10485760
```

- `binary` — путь к Pi, по умолчанию `pi`;
- `args` — дополнительные нерезервированные параметры;
- `session_dir` — каталог сессий;
- `project_trust` — `default`, `approve` или `deny`;
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
    protocol: takt-assistant/v1alpha1
    max_output_bytes: 10485760
```

Шаблоны `argv`:

- `{{prompt}}`;
- `{{run.id}}`, `{{node.id}}`, `{{attempt}}`;
- `{{model.name}}`, `{{model.id}}`, `{{model.provider}}`, `{{model.params}}`;
- `{{workspace}}`;
- `{{session.mode}}`, `{{session.id}}`.

Если protocol не задан и `{{prompt}}` отсутствует, prompt передаётся через stdin.

## Приоритет настроек

Для `command` и `prompt`:

1. `node.assistant` / `node.model`;
2. `assistant` / `model` во frontmatter команды;
3. `workflow.defaults.assistant` / `workflow.defaults.model`.

`session` задаётся в узле или `workflow.defaults`; значение по умолчанию — `fresh`.
