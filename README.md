# Takt

Takt — локальный Go-runtime для воспроизводимой оркестрации кодовых агентов,
детерминированных проверок, циклов, approval и долговременных запусков.

Takt не заменяет Pi, OpenCode или другой coding agent. Он управляет процессом
снаружи, а tool loop, работа с файлами, MCP, LSP, история и сжатие контекста
остаются внутри выбранного исполнителя.

> **Статус:** `v0.1.63-alpha`. Проект предназначен для локального
> однопользовательского trusted-режима. Workflow, конфигурация, подключаемые
> команды и рабочая директория должны быть доверенными. Сетевой и
> многопользовательский запуск не поддерживаются.

## Зачем нужен Takt

В агентной разработке стратегия часто оказывается зашита в приложение:
какой агент выполняет шаг, когда запускать проверки, что повторять после
ошибки и где ждать решения человека. Takt выносит эту координацию в
проверяемые Workflow и Config, сохраняя состояние, события и артефакты каждого
Run.

```text
Workflow + Config
       │
       ▼
  Takt scheduler ──► coding agent / model / script / adapter
       │
       ├── deterministic checks and retries
       ├── approval and resume
       └── durable state, events and artifacts
```

Подход проекта: **YAML координирует. Код вычисляет. Агент принимает решения.**

Takt подходит, когда процесс должен быть повторяемым, наблюдаемым и
продолжаемым после approval или сбоя. Для одиночного запроса к агенту без
workflow и долговременного состояния Takt обычно не нужен.

## Основные возможности

- DAG с зависимостями, условиями и параллельными волнами;
- `bash`, `script`, assistant, adapter и approval-узлы;
- retries, hooks, timeout, cancellation и durable backoff;
- `loop_group`, `foreach`, `matrix`, reusable `subworkflow` и governed child
  Run;
- локальный файловый Store с состоянием, событиями, usage и типизированными
  артефактами;
- адаптеры Pi и OpenCode, а также открытые process-протоколы и SDK;
- управляемые Git worktree, политики возможностей и ссылки
  `secret://ENV_NAME`;
- CLI, stdio MCP и локальный daemon через Unix socket;
- воспроизводимая evaluation поверх обычных Run.

Stable core отделён от extensions, experimental-функций и tooling. Точный
статус каждой области приведён в
[состоянии реализации](docs/05-implementation-status.md).

## Требования

- Go 1.23 или новее;
- Linux или macOS;
- Git для worktree-сценариев;
- Pi, OpenCode или совместимый process adapter — только для workflow с
  кодовым агентом.

## Установка

Готовые релизные бинарники и package-manager формулы пока не публикуются.
Соберите CLI из исходников:

```bash
git clone https://github.com/fedorovmv/takt.git
cd takt
make build
./bin/takt version
```

`make build` создаёт `bin/takt`. Подробности об обновлении и требованиях к
исполнителям находятся в [руководстве пользователя](docs/user-guide.md).

## Быстрый старт

Следующий пример не обращается к модели и показывает минимальный рабочий
контур `validate → run → inspect`.

```bash
mkdir -p /tmp/takt-demo/.takt
```

Сохраните в `/tmp/takt-demo/.takt/config.yaml`:

```yaml
apiVersion: takt/v1alpha1
kind: Config
```

Сохраните в `/tmp/takt-demo/workflow.yaml`:

```yaml
name: hello
nodes:
  - id: hello
    bash: printf 'hello from Takt\n'
    output_type: greeting
    output_mime: text/plain
```

Проверьте и запустите:

```bash
./bin/takt validate /tmp/takt-demo/workflow.yaml \
  --config /tmp/takt-demo/.takt/config.yaml \
  --workspace /tmp/takt-demo

./bin/takt run /tmp/takt-demo/workflow.yaml \
  --config /tmp/takt-demo/.takt/config.yaml \
  --workspace /tmp/takt-demo \
  --json
```

Команда `run` возвращает JSON с `id`. Используйте его для наблюдения:

```bash
./bin/takt run summary <run-id> --workspace /tmp/takt-demo --json=false
./bin/takt events <run-id> --workspace /tmp/takt-demo --json=false
./bin/takt artifacts <run-id> --workspace /tmp/takt-demo --json
```

Для запуска готового каталога процессов разработки установите профиль и
выберите Pi, OpenCode или совместимый adapter в `.takt/config.yaml`:

```bash
./bin/takt init code
./bin/takt workflow list code
./bin/takt validate code --workspace . --json
./bin/takt run code:assist --workspace . --input "Проверь состояние проекта" --json
```

Конфигурация исполнителя, approvals, daemon, MCP и эксплуатационные команды
описаны в [руководстве пользователя](docs/user-guide.md).

## Документация

| Раздел | Документ |
|---|---|
| Установка, настройка и использование | [Руководство пользователя](docs/user-guide.md) |
| Цель, сценарии и границы проекта | [Описание проекта](docs/01-project.md) |
| Архитектура | [Архитектура](docs/04-architecture.md) |
| Workflow и Config contract | [Спецификация `takt/v1alpha1`](docs/03-specification.md) |
| Статусы, retry, loops и resume | [Семантика runtime](docs/09-runtime-semantics.md) |
| Pi, OpenCode и process adapters | [Контракт assistant adapters](docs/10-assistant-adapter-spec.md) |
| Evaluation | [Руководство по evaluation](docs/73-evaluation-authoring-guide.md) |
| Текущее состояние и ограничения | [Состояние реализации](docs/05-implementation-status.md) |
| Направление развития | [Roadmap](docs/06-roadmap.md) |
| Полный индекс документации | [docs/README.md](docs/README.md) |
| Все источники истины и история | [Карта документации](docs/12-document-map.md) |

Рабочие примеры находятся в каталоге [`examples/`](examples). Для создания и
проверки собственных Workflow также доступен
[authoring skill](skills/takt/SKILL.md).

## Безопасность

Takt запускает доверенные локальные процессы с правами текущего пользователя.
Worktree и node policy не являются полной sandbox-границей. Передавайте
секреты через `secret://ENV_NAME`, не публикуйте daemon socket в сеть и не
принимайте workflow от недоверенных пользователей.

Полная модель угроз, доступные защиты и порядок сообщения об уязвимости
описаны в [SECURITY.md](SECURITY.md).

## Участие в разработке

Сообщения о дефектах, предложения и небольшие сфокусированные pull request
приветствуются. Перед изменением runtime или публичного контракта прочитайте
[CONTRIBUTING.md](CONTRIBUTING.md), [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) и
[DEVELOPMENT.md](DEVELOPMENT.md).

История пользовательских изменений ведётся в [CHANGELOG.md](CHANGELOG.md),
архитектурные решения — в
[ARCHITECTURE_DECISIONS.md](ARCHITECTURE_DECISIONS.md).

## Лицензия

Takt распространяется по лицензии [MIT](LICENSE). Атрибуция и сведения о
заимствованных идеях находятся в [NOTICE.md](NOTICE.md).
