# Локальное управление Takt через MCP

Запускай Takt как локальный stdio MCP-сервер:

```bash
takt mcp --workspace . --config .takt/config.yaml
```

Сервер публикует инструменты:

- `takt.workflow.list`, `takt.workflow.describe`;
- `takt.block.list`, `takt.block.describe`;
- `takt.plan`, `takt.plan.get`, `takt.execute`, `takt.run.steer`, `takt.plan.promote`;
- `takt.run.start`, `takt.run.get`, `takt.run.resume`;
- `takt.run.list`, `takt.run.attention`, `takt.run.summary`;
- `takt.run.pause`, `takt.run.resume_paused`, `takt.run.retry`, `takt.run.fork`, `takt.run.abandon`, `takt.run.recover`;
- `takt.run.answer`, `takt.run.cancel`;
- `takt.run.children`, `takt.run.artifacts`, `takt.run.events`;
- `takt.node.pending`, `takt.node.claim`, `takt.node.event`;
- `takt.node.tool.request`, `takt.node.tool.decide`, `takt.node.tool.start`, `takt.node.tool.complete`, `takt.node.tool.get`, `takt.node.tool.cancel`;
- `takt.node.artifact.declare`, `takt.node.complete`, `takt.node.fail`;
- `takt.host.begin`, `takt.host.confirm`, `takt.host.get`, `takt.host.find`, `takt.host.guard_tool`, `takt.host.guard_completion`, `takt.host.release`;
- `takt.notify.list`, `takt.notify.ack`, `takt.notify.test`.

`run.start` detached по умолчанию. Сохрани `run_id`, затем читай `run.get` и `run.events`. Для инкрементального чтения передавай `after_revision` из `next_revision`; `wait_ms` ограничен 30000.

Approval остаётся отдельным решением. Вызывай `run.answer` только после явного ответа пользователя, если текущий агент не получил отдельное разрешение на автоматическое подтверждение.

`run.artifacts` возвращает checksum и producer metadata. Используй `include_content` только для нужных результатов и задавай ограниченный `max_bytes`.

MCP-процесс локальный и доверенный. Не публикуй его в сеть и не передавай ему workflow от недоверенных пользователей.


## Dynamic Takt

`takt.block.list/describe` показывают только явно подключённые доверенные пакеты, effective governance, типизированные выходы и fingerprint каталога.

`takt.plan` принимает цель и возвращает решение `existing|planned`, preview, жёсткие бюджеты и `plan_id`. Для `planned` выполнение требует отдельного `takt.execute` с подтверждением. Прямой MCP ждёт terminal/waiting, а `takt mcp --daemon` возвращает управление после запуска и сохраняет Run независимо от клиента. `takt.plan.get` возвращает редакции плана, состояние фаз, связанные Run, usage и число артефактов. `takt.run.steer` сохраняет уточнение для ближайшего checkpoint; завершённые фазы не переписываются. `takt.plan.promote` доступен только для успешно завершённого task-specific плана и сохраняет повторно проверенный workflow проекта.

Основная сессия кодинг-агента не выполняет весь граф последовательно. Takt запускает отдельные Pi/OpenCode worker-сессии через обычные assistant nodes или выдаёт `executor: external` задания совместимому worker. Кодинг-агент остаётся интерфейсом пользователя: показывает preview, наблюдает события и артефакты, передаёт approval/steering и публикует итог.

## Внешний executor узла

Для `command`/`prompt` с `executor: external` агент должен: найти задачу через `takt.node.pending`, заявить её через `takt.node.claim` с фактической capability declaration, передавать message/usage/diagnostic через `takt.node.event`, а инструменты выполнять только через `takt.node.tool.request → decide → start → complete`. Созданный инструментом файл регистрируется через `takt.node.artifact.declare`; затем вызывается `takt.node.complete` или `takt.node.fail`. Claim token является секретом текущего lease и не должен попадать в сообщения или артефакты.


## Tool approval

При `tool_approval.mode: required` запрос инструмента блокируется до `takt.node.tool.decide`. Policy применяется раньше approval: запрещённый инструмент сразу получает `denied`. Внешний узел нельзя завершить, пока все tool calls не перешли в `completed`, `failed`, `denied` или `cancelled`.

## Постоянный локальный процесс

Когда Run должен пережить закрытие coding-agent host, запусти:

```bash
takt daemon start --workspace .
takt mcp --daemon --workspace .
```

Несколько локальных клиентов одного пользователя могут использовать один Unix socket. Для подписки без MCP используй `takt events <run-id> --daemon --follow`. Daemon не является сетевым сервером. После рестарта он не продолжает прежний OS-процесс, а выполняет PID-based recovery durable Run как новую attempt.

## Coding Agent Host Control

- `takt.host.begin` — создать durable managed session до вызова основной LLM;
- `takt.host.confirm` — подтвердить preview и запустить план;
- `takt.host.get`, `takt.host.find` — состояние и восстановление;
- `takt.host.guard_tool` — проверка tool call до исполнения;
- `takt.host.guard_completion` — запрет преждевременного final;
- `takt.host.release` — явный выход из managed mode без отмены Run.

Strict mode требует нативного host extension с command/input interception, tool/completion blocking и session recovery. Bundled Pi/OpenCode extensions в текущем релизе имеют уровень `guarded`.

## Autonomous Run Operations

`takt.run.list/attention/summary` дают реестр, очередь внимания и агрегированный результат. `pause` действует на границе узлов, `retry` повторяет failed node и зависимый хвост, `fork` создаёт новый запуск, `abandon` сохраняет историю в отдельном terminal-состоянии. Уведомления хранятся в durable inbox; desktop/process delivery является дополнительным sink, а не источником истины.
