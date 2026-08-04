# OpenCode diagnostics для provider-сбоев — v0.1.20-alpha

## Проблема

OpenCode может несколько раз повторять запрос к provider, писать предупреждения в stderr и публиковать структурированные `error` events в stdout. Если узел затем завершался по timeout, runtime сохранял только общую классификацию `timed_out`; сообщения о недоступном endpoint или ошибке соединения терялись на уровне состояния Run.

## Реализованная семантика

OpenCode adapter до возврата по timeout/cancellation:

- сохраняет raw stdout и stderr;
- извлекает неповторяющиеся сообщения из stderr;
- извлекает сообщения из доступных JSON events типа `error`;
- формирует краткий logical output с диагностикой;
- добавляет диагностику в context-ошибку.

Execution kind остаётся `timed_out` или `cancelled`. Диагностика дополняет причину, но не превращает timeout в `exit` или `protocol`.

Scheduler сохраняет специализированную context-ошибку adapter, если она уже имеет тот же authoritative kind. Производная exit/protocol ошибка по-прежнему заменяется завершившимся parent context.

## Сохраняемые данные

После provider timeout доступны:

- `NodeState.error` — timeout вместе с краткой provider-диагностикой;
- `NodeState.output` — краткая диагностика;
- `NodeState.stdout` — исходные NDJSON events;
- `NodeState.stderr` — исходные сообщения OpenCode;
- `NodeState.executions[].error` — причина конкретной попытки.

## Регрессии

Fake OpenCode воспроизводит сценарий:

```text
provider endpoint unavailable; retrying request 2/3
error event: dial tcp provider.example:443: connection refused
parent timeout
```

Adapter- и scheduler-тесты проверяют сохранение обоих сообщений при итоговом `timed_out`.

## Версия authoring skill

`skills/takt/VERSION`, `skills/takt/README.md` и версия поддерживаемого Takt теперь проверяются одной контрактной командой `scripts/test-takt-skill.sh`. Для этого релиза версия скилла — `0.2.1`, поддерживаемая версия Takt — `v0.1.20-alpha`.
