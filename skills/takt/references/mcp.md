# Локальное управление Takt через MCP

Запускай Takt как локальный stdio MCP-сервер:

```bash
takt mcp --workspace . --config .takt/config.yaml
```

Сервер публикует инструменты:

- `takt.workflow.list`, `takt.workflow.describe`;
- `takt.run.start`, `takt.run.get`, `takt.run.resume`;
- `takt.run.answer`, `takt.run.cancel`;
- `takt.run.children`, `takt.run.artifacts`, `takt.run.events`;
- `takt.node.pending`, `takt.node.claim`, `takt.node.event`, `takt.node.complete`, `takt.node.fail`.

`run.start` detached по умолчанию. Сохрани `run_id`, затем читай `run.get` и `run.events`. Для инкрементального чтения передавай `after_revision` из `next_revision`; `wait_ms` ограничен 30000.

Approval остаётся отдельным решением. Вызывай `run.answer` только после явного ответа пользователя, если текущий агент не получил отдельное разрешение на автоматическое подтверждение.

`run.artifacts` возвращает checksum и producer metadata. Используй `include_content` только для нужных результатов и задавай ограниченный `max_bytes`.

MCP-процесс локальный и доверенный. Не публикуй его в сеть и не передавай ему workflow от недоверенных пользователей.

## Внешний executor узла

Для `command`/`prompt` с `executor: external` агент должен: найти задачу через `takt.node.pending`, заявить её через `takt.node.claim` с фактическими capabilities, передавать tool/usage/diagnostic события через `takt.node.event`, затем вызвать `takt.node.complete` или `takt.node.fail`. Claim token является секретом текущего lease и не должен попадать в сообщения или артефакты.
