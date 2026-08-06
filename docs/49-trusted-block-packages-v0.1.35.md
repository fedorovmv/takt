# Доверенные пакеты блоков Dynamic Takt — v0.1.35-alpha

## Цель среза

Срез добавляет минимальный пакетный слой, необходимый Dynamic Takt уже сейчас. Полный установщик, удалённый реестр и управление зависимостями остаются последующим направлением. Текущий контракт решает более узкую задачу: организация явно подключает проверенные блоки и ограничения, а планировщик может строить процесс только из этого доверенного каталога.

Пакет может содержать:

- блоки исследования, реализации, проверки и сведения результатов;
- шаблоны инженерных документов;
- обязательные блоки и проверки;
- правила веток;
- шаблон запроса на изменение;
- разрешённые интеграции;
- общую политику инструментов, навыков, MCP и песочницы;
- максимальные бюджеты дочерних Run, параллельности, редакций и токенов.

## Подключение пакетов

Профиль объявляет упорядоченный список доверенных пакетов:

```yaml
apiVersion: takt/v1alpha1
kind: Profile
metadata:
  name: code
workflow: workflow.yaml
config: config.yaml
block_packages:
  - workflows/blocks/package.yaml
  - ../../packages/corporate-engineering/package.yaml
```

Путь может указывать на встроенный, корпоративный или проектный пакет. Подключение является явным: Dynamic Takt не ищет блоки в рабочей директории автоматически.

Команды:

```bash
takt block validate package.yaml
takt block list --profile code --workspace .
takt block describe corp-validate --profile code --workspace .
```

Те же операции доступны через MCP:

- `takt.block.list`;
- `takt.block.describe`.

## Формат BlockPackage

```yaml
apiVersion: takt/v1alpha1
kind: BlockPackage
metadata:
  name: corporate-engineering
  version: 1.0.0
  scope: corporate

blocks:
  corp-research:
    workflow: research.yaml
    capabilities: [repository.read, corporate.docs.read]
    integrations: [filesystem, tracker]
    output_paths: [summary, findings, evidence]

  corp-validate:
    workflow: validate.yaml
    capabilities: [repository.read, shell, ci.read]
    integrations: [filesystem, ci]
    output_paths: [summary, passed, checks, evidence]

templates:
  research: corporate-evidence-report-v1
  change_request: corporate-change-request-v3

governance:
  required_blocks: [corp-validate]
  required_checks: [make-check, unit-race, security-policy]
  allowed_integrations: [filesystem, git, tracker, scm, ci]
  branch_rules:
    prefix: feature/
    pattern: "^feature/[a-z0-9-]+$"
    require_clean_base: true
  change_request_template: corporate-change-request-v3
  policy:
    denied_tools: [network-unapproved]
    requires: [tool_policy, sandbox_filesystem]
  limits:
    max_child_runs: 48
    max_parallel: 8
    max_iterations: 4
    max_tokens: 1500000
```

Машиночитаемый контракт находится в `schemas/block-package.schema.json`. Полный пример — `examples/corporate-block-package/`.

## Объединение ограничений

Несколько явно подключённых пакетов образуют один каталог.

- Имена блоков обязаны быть уникальными.
- Обязательные блоки и проверки объединяются.
- Максимальные бюджеты сужаются до минимального положительного значения.
- Списки разрешённых интеграций пересекаются.
- Запрещённые инструменты и требования объединяются.
- Разрешённые инструменты и навыки пересекаются.
- Более строгая файловая или сетевая политика сохраняется.
- Конфликтующие правила веток и шаблоны запроса на изменение отклоняются.

Корпоративная политика применяется и к встроенным блокам. Поэтому встроенный `implement` не может обойти общий корпоративный запрет только потому, что он определён в другом пакете.

## Проверка доверенного блока

При загрузке каталога Takt проверяет:

- принадлежность workflow каталогу пакета;
- обычную схему и семантику workflow;
- ровно один публичный итоговый узел;
- наличие каждого `output_path` в `output_format` итогового узла;
- тип объявленного результата;
- соответствие интеграций allowlist пакета;
- отсутствие governed child Run внутри блока.

Последнее ограничение сохраняет прозрачный бюджет Dynamic Takt: дочерние Run создаются фазами `WorkflowPlan`, а пакетный блок не может скрыто породить дополнительное дерево запусков. Inline-композиция в пределах того же Run остаётся обычной семантикой workflow.

Для `map` источник должен точно совпадать с объявленным `output_path` предыдущего блока и иметь тип `array`. Проверки по одному только тексту prompt недостаточно.

## Связь с Dynamic Takt

При планировании Takt передаёт модели только фактически подключённый каталог:

- имена и описания блоков;
- возможности и интеграции;
- типизированные выходы;
- шаблоны;
- обязательные проверки;
- корпоративные ограничения и бюджеты.

План проходит `ValidateWithCatalog`, после чего компилируется в обычный Takt Workflow. Пути workflow и effective policy берутся из каталога, а не строятся из имени блока.

План сохраняет:

- список путей пакетов;
- общий SHA-256 fingerprint каталога.

Fingerprint включает package manifest и транзитивное содержимое, адресуемое блоком: expanded subworkflow, Markdown-команды, script source/dependencies, path skills и MCP-конфигурации. Их изменение после preview блокирует execute, replan и promote. Новый состав пакетов требует нового плана.

Preview показывает требуемые возможности и интеграции наряду с фазами и бюджетами.

## Точные границы governance

- Отсутствующий `allowed_integrations` не добавляет package-level ограничения; явный `allowed_integrations: []` запрещает все интеграции.
- `max_tokens: 0` в пользовательском `WorkflowPlan` нормализуется в bounded default. Ноль в `BlockPackage.governance.limits` означает, что пакет не добавляет верхнюю границу; итоговый план всё равно обязан иметь положительный лимит. Та же package-семантика применяется к остальным limits.
- `capabilities` участвуют в preflight, `integrations` — в allowlist каталога. `required_checks`, `branch_rules` и `change_request_template` являются метаданными управления: enforcement появляется только через обязательный block или будущий domain adapter.
- Профиль без `block_packages` сохраняет статические workflow, но `takt plan` не может построить planned-процесс до явного подключения каталога.
- `takt block --json` без subcommand эквивалентен `takt block list --json`.
- Каталог пока перечитывается и пересчитывает fingerprint на `plan.get/preview`; кеш по fingerprint отложен.

## Исправления Dynamic Takt v0.1.34

### Исполнение CLI

`takt execute` без daemon теперь является передним запуском: команда выполняет сегменты до завершения либо устойчивого ожидания пользователя. Она не оставляет `running`-план после завершения собственного процесса.

Для фонового режима используется:

```bash
takt execute <plan-id> --confirm --daemon --workspace .
```

MCP через daemon создаёт отсоединённое исполнение. Прямой stdio MCP выполняет план в процессе запроса и возвращает terminal либо waiting-состояние.

### Редакции и steering

Лимит `max_iterations` применяется до любого перепланирования, включая `ask_user → steer`. Steering помечается применённым только после успешной проверки решения. Невалидное `replace_remaining` оставляет сообщение доступным для следующей попытки.

### Бюджеты

В `max_child_runs` и `max_tokens` входят:

- планировщик;
- перепланировщики;
- сегментные Run;
- governed child Run фаз;
- элементы fan-out.

`max_tokens: 0` нормализуется в ограниченное значение и не означает безлимитный запуск. `max_parallel` ограничивает не только fan-out, но и независимые task-фазы сегмента через устойчивые параллельные линии.

### Авторинг и runtime

- Анализатор `when` проверяет все части выражений с `&&` и `||`.
- Artifact path на macOS разрешает существующий символьный префикс, поэтому ещё не созданный файл под `/var` корректно сопоставляется с `/private/var`.
- Продвижение плана не перезаписывает существующий workflow без `--force`.
- Daemon и прямой MCP используют межпроцессный lock продвижения динамических планов.
- `adversarial-verify` получил отдельный блок.
- `reason` обязателен и в Go-валидаторе.
- Ошибки сохранения состояния не подавляются вторичной записью.

## Границы текущего пакета

Текущий срез не реализует:

- install/update/uninstall;
- удалённый реестр пакетов;
- lock-файл версий и зависимостей;
- подписи и цепочку поставки;
- автоматическое разрешение конфликтов корпоративных политик;
- область `global` как отдельный менеджер установки.

Пакеты находятся локально, явно подключаются профилем и считаются доверенными файлами текущего пользователя. Это достаточная граница для корпоративного каталога блоков Dynamic Takt; полноценная доставка пакетов остаётся отдельным продуктовым срезом.
