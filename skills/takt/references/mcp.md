# Локальный MCP Takt

## Поверхности

Основная сессия кодинг-агента запускает безопасную компактную поверхность:

```bash
takt mcp --workspace .                 # то же, что --surface agent
takt mcp --surface agent --workspace .
```

Она публикует только пять высокоуровневых операций:

- `takt.task.start` — выбрать маршрут, построить preview и при `go: true` запустить процесс;
- `takt.task.status` — получить краткое состояние и признак `needs_input`;
- `takt.task.respond` — ответить на вопрос, подтвердить действие или передать steering;
- `takt.task.stop` — остановить активную задачу;
- `takt.task.explain` — показать route, controls, фазы, связанные Run и артефакты.

Внутренние протоколы разделены по роли клиента:

```bash
takt mcp --surface host       # host bridge Pi/OpenCode/другого coding-agent
takt mcp --surface worker     # внешний executor узла
takt mcp --surface operator   # CLI/daemon сопровождение
takt mcp --surface all        # совместимость и диагностика
```

Поверхность задаётся конфигурацией процесса, а не аргументом tool call. Основная LLM не должна видеть `takt.host.*`, `takt.node.*`, notification delivery или низкоуровневые recovery-операции.

## Task Router

`takt.task.start` передаёт задачу внутреннему Task Router. Приоритет маршрутов:

```text
готовый workflow
→ stable template simple-reliable
→ bounded Dynamic Plan
```

Семантический router не выдаёт полномочия. Go-код повторно проверяет workflow, доверенные blocks, параметры и минимальные controls. При ошибке router Takt выбирает `simple-reliable + inspect_first`, фиксирует fallback в route и продолжает обычную задачу.

## Daemon

Для Run, который должен пережить закрытие coding-agent host:

```bash
takt daemon start --workspace .
takt mcp --daemon --surface agent --workspace .
```

Daemon выбирает поверхность по локальному запросу. Его HTTP/MCP compatibility endpoint без заголовка сохраняет `all` для старых клиентов; новые мосты должны указывать свою роль явно. Daemon не является сетевым сервером.

## Операторская поверхность

Оператор получает discovery и полное управление WorkflowPlan/Run без host и worker протоколов:

- workflow/block discovery: `takt.workflow.list`, `takt.workflow.describe`, `takt.block.list`, `takt.block.describe`;
- plan lifecycle: `takt.plan`, `takt.plan.get`, `takt.execute`, `takt.run.steer`, `takt.plan.promote`;
- Run lifecycle: `takt.run.start`, `takt.run.get`, `takt.run.list`, `takt.run.attention`, `takt.run.summary`, `takt.run.events`, `takt.run.artifacts`;
- управление: pause/resume/retry/fork/abandon/recover/cancel/answer;
- notification inbox и acknowledgement.

`run.start` и `execute` через daemon возвращают управление после durable запуска. Сохраняй `run_id`, читай события по `revision` cursor и запрашивай содержимое только нужных артефактов с ограниченным `max_bytes`.

## Host control

Host bridge использует отдельную поверхность:

- `takt.host.begin`, `confirm`, `get`, `find`;
- `takt.host.guard_tool`, `guard_completion`;
- `takt.host.release`.

Strict mode требует нативного перехвата команды и ввода до LLM, блокирующего tool/completion gate и восстановления сессии. Bundled Pi/OpenCode extensions остаются `guarded` до live smoke на зафиксированных версиях. Другие хосты подключаются собственным bridge и не требуют изменения workflow Takt.

## Внешний executor узла

Worker surface публикует только lifecycle узла:

- `takt.node.pending`, `claim`, `event`;
- `takt.node.tool.request`, `decide`, `start`, `complete`, `get`, `cancel`;
- `takt.node.artifact.declare`;
- `takt.node.complete`, `fail`;
- `takt.node.reconcile` для `side_effect.mode: reconcile` после неизвестного исхода внешней мутации.

Claim token является секретом lease. Инструмент выполняется только после `request → policy/approval decision → start → complete`. Узел нельзя завершить, пока все tool calls не достигли terminal-состояния. Для non-idempotent внешнего действия в режиме `reconcile` истёкший claim не переигрывается: adapter сначала сообщает `not_applied|applied|unknown`; `applied` требует receipt.

## Безопасность

MCP-процесс локальный и доверенный. Не публикуй его в сеть и не передавай ему workflow от недоверенных пользователей. Surface уменьшает доступный контракт, но не заменяет sandbox и права операционной системы.


### Structured task input

`takt.task.start` принимает либо `goal`, либо пару `source` + `source_ref`. Второй путь вызывает configured Task Source adapter до Router; остальные операции `takt.task.*` остаются теми же.
