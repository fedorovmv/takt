# Dynamic Takt и интеграция с кодинг-агентом — v0.1.34-alpha

## Цель среза

Срез добавляет один полный пользовательский путь: основная сессия Pi/OpenCode получает сложную задачу, просит Takt выбрать готовый процесс либо построить ограниченный task-specific план, показывает preview и бюджет, запускает процесс после подтверждения, наблюдает фазы и артефакты, передаёт уточнения в контрольные точки и при успехе продвигает план в workflow проекта.

Takt не становится кодинг-агентом и не реализует собственный tool loop. Он управляет обычными Run и отдельными сессиями Pi/OpenCode, которые читают и изменяют код, вызывают shell, Git и MCP своими штатными инструментами.

## Интеллектуальное решение

Новый высокоуровневый вызов возвращает одно из двух решений:

- `existing` — задача соответствует одному именованному workflow профиля;
- `planned` — требуется временный процесс, собранный из разрешённых блоков.

```bash
takt plan "Проверь совместимость MCP-инструментов" --workspace .
```

Для `existing` Takt проверяет селектор профиля. Для `planned` план проходит нормализацию и строгую проверку до создания execution Run.

## Ограниченный WorkflowPlan

`WorkflowPlan` — промежуточный план компиляции, а не второй runtime и не произвольный Takt YAML.

```json
{
  "apiVersion": "takt/v1alpha1",
  "kind": "WorkflowPlan",
  "decision": "planned",
  "goal": "Проверить совместимость MCP-инструментов",
  "reason": "Нужны инвентаризация, параллельная проверка и независимое сведение",
  "budget": {
    "max_child_runs": 24,
    "max_parallel": 4,
    "max_iterations": 3,
    "max_tokens": 500000
  },
  "phases": [
    {
      "id": "inventory",
      "uses": "discover",
      "objective": "Найти MCP-инструменты",
      "strategy": "task",
      "checkpoint": true
    },
    {
      "id": "inspect",
      "uses": "investigate",
      "objective": "Проверить один инструмент",
      "depends_on": ["inventory"],
      "strategy": "map",
      "source": "phases.inventory.output.items",
      "max_parallel": 4
    },
    {
      "id": "summary",
      "uses": "synthesize",
      "objective": "Свести совместимость и сложные расхождения",
      "depends_on": ["inspect"],
      "strategy": "task"
    }
  ]
}
```

Разрешённые блоки профиля `code`:

- `discover`;
- `investigate`;
- `implement`;
- `validate`;
- `review`;
- `adversarial-verify`;
- `synthesize`.

Ядро знает только структурные свойства фаз: обычная задача, `map`, зависимости и checkpoint. Семантика исследования, реализации и ревью находится в workflow-блоках профиля.

## Компиляция и исполнение

Проверенный план делится на сегменты по явным checkpoint. Каждый сегмент компилируется в обычный `takt/v1alpha1 Workflow`:

- обычная фаза становится governed `workflow`-узлом;
- `map` становится `workflow.fan_out`;
- зависимости сохраняются внутри сегмента;
- блоки запускаются отдельными child Run с `isolation: inherit`;
- существующие runtime, events, artifacts, retry, cancellation и worktree остаются единственным исполнительным контуром.

Отдельный dynamic runtime не вводится.

## Preview, подтверждение и бюджеты

`planned`-решение сохраняется со статусом `draft` и не запускается без подтверждения:

```bash
takt execute <plan-id> --workspace . --confirm
```

Preview показывает фазы, `map`-параллельность, checkpoint и жёсткие пределы. Runtime ограничивает:

- суммарный бюджет child Run;
- число элементов каждого fan-out в пределах оставшегося бюджета;
- параллельность;
- число редакций плана;
- суммарный token usage завершённых execution Run на границе фазы.

## Контрольные точки и steering

Перепланирование выполняется только после фазы с `checkpoint: true`. Планировщик получает исходную цель, текущую редакцию, завершённые фазы, результаты, оставшиеся фазы, остаток бюджета и новые steering-сообщения.

Разрешённые решения:

- `continue`;
- `replace_remaining`;
- `finish`;
- `ask_user`.

`replace_remaining` создаёт новую revision. Завершённые фазы и их Run не переписываются. Уточнение передаётся командой:

```bash
takt steer <plan-id> "Исправляй только простые расхождения" --workspace .
```

Если процесс уже ждёт пользователя в checkpoint, steering немедленно передаётся планировщику. Иначе сообщение сохраняется до ближайшей контрольной точки.

## Наблюдение

```bash
takt plan get <plan-id> --workspace .
```

Представление содержит:

- текущую и прошлые редакции;
- статус каждой фазы;
- связанные execution Run;
- текущий узел;
- usage;
- число артефактов;
- steering и последнюю ошибку.

Подробные сообщения и tool events читаются существующим `takt.run.events`, а содержимое результатов — `takt.run.artifacts`.

## Продвижение процесса

Успешный `planned`-процесс можно преобразовать в проектный workflow:

```bash
takt plan promote <plan-id> \
  --name audit-mcp-compatibility \
  --workspace .
```

Takt компилирует последнюю редакцию с `${input}` вместо конкретной исходной цели, сохраняет файл в `.takt/workflows/generated/` и повторно загружает его через штатный валидатор. Продвижение доступно только для completed-плана.

## MCP для кодинг-агента

Добавлены высокоуровневые инструменты:

- `takt.plan`;
- `takt.plan.get`;
- `takt.execute`;
- `takt.run.steer`;
- `takt.plan.promote`.

Основная сессия кодинг-агента показывает preview, подтверждает запуск, наблюдает события и передаёт steering. Фазы выполняют отдельные Pi/OpenCode worker-сессии либо совместимый `executor: external`; основная сессия не должна блокирующе выполнять весь граф сама.

## Исправления daemon и открытых дефектов v0.1.32

- Длинный Unix socket path получает детерминированный fallback в `$TMPDIR/takt-daemon/<workspace-hash>/daemon.sock`; metadata и lock остаются в workspace. Явно заданный слишком длинный путь отклоняется до bind с понятной ошибкой.
- Event subscription перед terminal close дочитывает журнал до revision состояния, поэтому `run.completed` не теряется в гонке между `Events()` и `GetRun()`.
- External claim хранит `claimed_at`; legacy claim без `last_activity_at` и `claimed_at` не получает выдуманное время из фиксированных 15 минут.
- Process assistant v1alpha2 завершает запущенный процесс на любой protocol-ошибке до нормального `Wait`.
- Governed child Run повторно проверяет input schema после шаблонного рендеринга.
- Review/approval-ветви профиля получили явные `when` и непустой input.
- `validation_commands` исполняются последовательно специальным детерминированным `script.runtime: validation`; их результат участвует в validation/recovery gate.
- `when` поддерживает ограниченные логические `&&` и `||` поверх существующих сравнений.
- Ответы concurrent JSON-RPC proxy могут приходить не в порядке запросов; это допустимая семантика JSON-RPC и теперь зафиксирована в документации.

## Границы

- Планировщик работает только с разрешённым каталогом блоков и не генерирует произвольный код оркестратора.
- Перепланирование возможно только в checkpoint.
- Token limit проверяется на границах фаз; принудительная остановка внутри уже запущенного provider-вызова остаётся задачей adapter/runtime hardening.
- Продвижение компилирует последнюю редакцию, но не заменяет инженерное ревью созданного проектного процесса.
- Dynamic Takt остаётся локальным trusted runtime без Web UI, БД и внешних message adapters.
