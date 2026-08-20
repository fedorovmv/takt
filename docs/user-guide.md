# Руководство пользователя

Это руководство описывает установку, настройку и повседневное использование
Takt `v0.1.63-alpha`. Точный Workflow/Config contract находится в
[`03-specification.md`](03-specification.md), а полный список текущих
ограничений — в [`05-implementation-status.md`](05-implementation-status.md).

## 1. Модель использования

В одном workspace Takt читает Workflow и Config, создаёт Run и сохраняет его
состояние в `.takt/`:

```text
workspace/
  .takt/
    config.yaml
    profiles/
    runs/
    worktrees/
  workflow.yaml
```

Workflow задаёт граф и правила выполнения. Config связывает логические модели
и providers с конкретными исполнителями. Run содержит состояние узлов,
события, usage, approvals и артефакты.

Takt рассчитан на доверенный локальный workspace. Он запускает scripts и
coding agents с правами текущего пользователя; подробнее см.
[`SECURITY.md`](../SECURITY.md).

Команды `takt task`, `plan`, `execute`, `steer`, `host` и `learn` относятся к
experimental surface и намеренно не входят в основной пользовательский
маршрут этого руководства. Их контракт может меняться до перевода в stable.

## 2. Требования

Для сборки и детерминированных workflow нужны:

- Go 1.23 или новее;
- Linux или macOS;
- Git, если workflow использует managed worktree.

Для assistant-узлов дополнительно установите хотя бы один поддерживаемый host:

- [Pi](10-assistant-adapter-spec.md#9-pi-adapter);
- [OpenCode](10-assistant-adapter-spec.md#10-opencode-adapter);
- внешний process adapter с протоколом `takt-assistant/v1alpha1` или
  `takt-assistant/v1alpha2`.

Takt не устанавливает host и не управляет его учётными данными. Авторизация
остаётся ответственностью выбранного исполнителя.

## 3. Установка и обновление

Клонируйте репозиторий и соберите бинарник:

```bash
git clone https://github.com/fedorovmv/takt.git
cd takt
make build
./bin/takt version
```

Скопируйте `bin/takt` в каталог из `PATH`, если хотите запускать его как
`takt`. Во время alpha-линии удалённый `go install ...@latest`, готовые
бинарники и package-manager формулы не поддерживаются: module path пока
предназначен для сборки из checkout.

Для обновления получите нужную revision и пересоберите CLI:

```bash
git pull --ff-only
make build
./bin/takt version
```

Перед обновлением прочитайте [`CHANGELOG.md`](../CHANGELOG.md). Не удаляйте
проектный `.takt/`, если нужно сохранить Run history. Для удаления CLI
достаточно удалить установленный бинарник; данные workspace остаются отдельно.

## 4. Первый локальный Run

Создайте минимальный Config:

```yaml
apiVersion: takt/v1alpha1
kind: Config
```

Сохраните его как `.takt/config.yaml`, а рядом создайте `workflow.yaml`:

```yaml
name: hello
description: Minimal deterministic workflow
nodes:
  - id: hello
    bash: printf 'hello from Takt\n'
    output_type: greeting
    output_mime: text/plain
```

Проверьте определение до создания Run:

```bash
takt validate workflow.yaml --workspace . --warnings-as-errors
```

Запустите workflow:

```bash
takt run workflow.yaml --workspace . --json
```

Ответ содержит устойчивый `id`. Состояние и результат сохраняются в
`.takt/runs/<run-id>/`; читать внутренние файлы напрямую обычно не нужно.

## 5. Настройка моделей и coding agent

### OpenCode

Пример минимальной конфигурации:

```yaml
apiVersion: takt/v1alpha1
kind: Config
default_assistant: opencode

models:
  implementation:
    provider: openai
    id: replace-with-your-model

assistants:
  opencode:
    type: opencode
    binary: opencode
    agent: build
    auto_approve: false
    max_output_bytes: 10485760
```

`auto_approve: true` снимает внешний approval boundary OpenCode. Используйте
его только в доверенном workspace после проверки конфигурации. Рабочий smoke
пример находится в [`examples/opencode-smoke/`](../examples/opencode-smoke/).

### Pi

```yaml
apiVersion: takt/v1alpha1
kind: Config
default_assistant: pi

models:
  implementation:
    provider: openai
    id: replace-with-your-model

assistants:
  pi:
    type: pi
    binary: pi
    project_trust: deny
    max_output_bytes: 10485760
```

Pi запускается через RPC mode. `project_trust: deny` сохраняет внешний
trust boundary; `approve` допустим только для проверенного проекта. Рабочий
пример находится в [`examples/pi-smoke/`](../examples/pi-smoke/).

### Assistant-узел

Workflow с прямым prompt ссылается на provider и логический alias модели:

```yaml
name: summarize
provider: opencode
model: implementation
nodes:
  - id: summary
    prompt: |
      Inspect the current workspace and return a concise factual summary.
    timeout: 5m
```

Профиль `code` использует логическое имя `coding-agent`; в этом случае
конкретный host выбирает `default_assistant`. Для сторонних CLI используйте
открытый process protocol и
[`sdk/agentadapter`](../sdk/agentadapter). Примеры находятся в
[`examples/agent-session-adapters/`](../examples/agent-session-adapters/) и
[`examples/reference-adapters/`](../examples/reference-adapters/).

### Model presets

Если окружения используют разные наборы моделей, Config может объявить
`model_presets`, а запуск — выбрать один из них:

```bash
takt validate code --model-preset local --workspace .
takt run code:assist --model-preset local --workspace . --input request.md
```

Разовые переопределения задаются повторяемым флагом
`--model alias=provider/model-id`. Полный контракт и правила взаимного
исключения `models`/`model_presets` описаны в
[`03-specification.md`](03-specification.md#3-конфигурация-моделей-и-исполнителей).

## 6. Создание Workflow

Узел определяет ровно одно действие: assistant command/prompt, `bash`,
`script`, `adapter`, `approval`, `loop_group`, `subworkflow`, `foreach`,
`matrix`, governed `workflow` или `assessment`.

Пример с зависимостью и проверкой:

```yaml
name: build-and-check
nodes:
  - id: build
    bash: go build ./...
  - id: test
    depends_on: [build]
    bash: go test ./...
```

Полезные правила authoring:

- независимые готовые узлы выполняются параллельными волнами;
- `$build.output` — обязательная ссылка, `$build.output?` — optional,
  `$build.output:-default` — явный fallback;
- сложное вычисление выполняйте в `bash`/`script`, а не расширяйте `when`;
- structured JSON закрепляйте через `output_format`;
- значимые файлы публикуйте как typed artifacts;
- результат агента подтверждайте детерминированным gate;
- выполняйте `takt validate` после каждого изменения определения.

Подробные паттерны находятся в
[`skills/takt/references/workflows.md`](../skills/takt/references/workflows.md),
а полная схема — в [`schemas/workflow.schema.json`](../schemas/workflow.schema.json).

## 7. Профиль процессов разработки

Встроенный профиль `code` устанавливает 19 именованных процессов и отдельный
default router. Поэтому `takt workflow list code` выводит 20 записей:

```bash
takt init code
takt workflow list code
takt workflow describe code:feature-development
```

Команда без suffix запускает router, явный selector выбирает процесс напрямую:

```bash
takt run code --workspace . --input request.md --json
takt run code:assist --workspace . --input "Объясни этот проект" --json
```

Перед первым запуском замените пример моделей в `.takt/config.yaml` и выберите
реально установленный `default_assistant`. Повторный
`takt init code --force` заменяет локальные файлы профиля; сначала сохраните
свои изменения.

Описание workflow и входных контрактов находится в установленном
`.takt/profiles/code/README.md` и в
[`internal/profile/builtin/code/README.md`](../internal/profile/builtin/code/README.md).

## 8. Наблюдение и управление Run

Основные команды не требуют знания формата Store:

```bash
takt run summary <run-id> --workspace . --json=false
takt run status <run-id> --workspace . --json=false
takt run inspect <run-id> --workspace . --json=false
takt events <run-id> --workspace . --json=false
takt artifacts <run-id> --workspace . --recursive --json
takt children <run-id> --workspace . --json
```

Для отмены дерева Run:

```bash
takt cancel <run-id> --workspace . --reason "no longer needed"
```

Если Run ждёт approval, `run summary` показывает node ID и сообщение. Передайте
решение и продолжите ту же итерацию:

```bash
takt answer <run-id> <node-id> --workspace . --value approved
```

`takt resume <run-id>` предназначен для Run, который можно продолжить без
нового approval value. Не редактируйте `state.json` вручную.

## 9. Фоновый запуск и daemon

Daemon нужен, когда Run должен пережить закрытие клиента или к одному workspace
подключаются несколько локальных клиентов текущего пользователя:

```bash
takt daemon start --workspace .
takt daemon status --workspace .
takt run workflow.yaml --workspace . --daemon --json
takt events <run-id> --workspace . --daemon --follow --json=false
takt daemon stop --workspace .
```

Daemon использует `.takt/daemon.sock` и тот же файловый Store, что CLI. Это не
сетевой сервис и не security boundary. Не проксируйте socket в TCP и не
размещайте workspace в каталоге, доступном недоверенным пользователям.

Операционные команды для длительных Run:

```bash
takt run list --active --workspace . --daemon
takt run attention --workspace . --daemon
takt run pause <run-id> --workspace . --daemon
takt run resume <run-id> --workspace . --daemon
takt run retry <run-id> --node <node-id> --workspace . --daemon
```

## 10. MCP

Одноразовый stdio MCP для coding-agent host:

```bash
takt mcp --surface agent --workspace . --config .takt/config.yaml
```

Через daemon:

```bash
takt daemon start --workspace .
takt mcp --surface agent --workspace . --daemon
```

Agent surface по умолчанию содержит только высокоуровневые `takt.task.*`
операции. `host`, `worker`, `operator` и `all` предназначены для отдельных
доверенных потребителей. Список и схемы canonical operations находятся в
[`71-canonical-operation-contracts.generated.md`](71-canonical-operation-contracts.generated.md),
практическая настройка — в
[`skills/takt/references/mcp.md`](../skills/takt/references/mcp.md).

## 11. Worktree и артефакты

Workflow может включить managed Git worktree. Разовый выбор доступен через
`--worktree`/`--no-worktree`:

```bash
takt run workflow.yaml --workspace . --worktree --keep-worktree
takt worktree list --workspace .
takt worktree prune --workspace .
```

Worktree изолирует изменения от control checkout, но не ограничивает доступ
процесса к файловой системе или сети. Failed и dirty worktree сохраняются для
расследования.

Typed artifacts принадлежат producer Run и имеют MIME, SHA-256 и metadata.
Получайте их через `takt artifacts`, а не угадывайте внутренние пути Store.

## 12. Секреты и sandbox

Не помещайте credentials в Workflow, task input или `models.*.params`.
Передавайте их через environment-backed ссылку:

```yaml
script:
  runtime: command
  path: ./scripts/check.sh
  env:
    TOKEN: secret://SERVICE_TOKEN
```

Takt редактирует известные секреты перед persistence, но transformed или
неизвестное значение может не совпасть с redactor. Полные ограничения описаны
в [`SECURITY.md`](../SECURITY.md).

Для локальных deterministic `bash`/`script` узлов можно запросить OS sandbox:

```yaml
sandbox:
  enforcement: required
  filesystem: read_only
  network: deny
```

`required` завершается до запуска payload, если `bwrap` на Linux или
`sandbox-exec` на macOS недоступен. Для assistant-узлов sandbox остаётся
capability contract выбранного adapter.

## 13. Диагностика

Начинайте с этих команд:

```bash
takt validate <workflow> --workspace . --warnings-as-errors --json
takt run summary <run-id> --workspace . --json=false
takt events <run-id> --workspace . --json=false
takt compatibility check --config .takt/config.yaml
```

Частые причины ошибок:

- **Config не найден:** проверьте `--workspace` и путь `.takt/config.yaml`;
- **unknown provider/model:** исправьте alias и проверьте доступные модели
  самого Pi/OpenCode;
- **unknown assistant `coding-agent`:** задайте установленный Pi/OpenCode или
  process adapter в `default_assistant`;
- **unsupported capability:** выбранный adapter не может обеспечить policy
  узла; ослаблять обязательную policy молча нельзя;
- **fingerprint changed:** определение изменилось после Run; создайте новый Run
  либо восстановите исходную revision;
- **dirty worktree:** зафиксируйте/уберите изменения или используйте
  `--allow-dirty-worktree`, только если осознанно принимаете старт от HEAD;
- **Run waiting:** прочитайте `run summary` и ответьте на указанный approval;
- **required sandbox unavailable:** установите backend или измените policy,
  если degraded execution действительно допустим.

Расширенный список находится в
[`skills/takt/references/troubleshooting.md`](../skills/takt/references/troubleshooting.md).

## 14. Следующие материалы

- [`examples/`](../examples) — выполняемые примеры по отдельным возможностям;
- [`03-specification.md`](03-specification.md) — полный Workflow/Config
  contract;
- [`09-runtime-semantics.md`](09-runtime-semantics.md) — terminal status,
  retries, loops и resume;
- [`10-assistant-adapter-spec.md`](10-assistant-adapter-spec.md) — protocol и
  host-specific semantics;
- [`73-evaluation-authoring-guide.md`](73-evaluation-authoring-guide.md) —
  production evaluation;
- [`12-document-map.md`](12-document-map.md) — все источники истины и история.
