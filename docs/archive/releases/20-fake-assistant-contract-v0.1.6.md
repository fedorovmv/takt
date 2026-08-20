# Fake-assistant contract suite в v0.1.6-alpha

## Цель

Зафиксировать нормализованный контракт внешнего исполнителя до реализации Pi/OpenCode adapter.

## Реализованный протокол

Конфигурация process assistant может включать:

```yaml
protocol: takt-assistant/v1alpha1
```

В этом режиме Takt передаёт через stdin один JSON request envelope и ожидает в stdout один JSON result envelope. Схема: `schemas/assistant-protocol.schema.json`.

Протокол включает:

- Run ID, Node ID и номер попытки;
- prompt и workspace;
- логическую и физическую модель;
- `fresh`/`resume` session;
- native hooks, metadata и limits;
- output, structured result, session ID, `resumed`, usage и resolved model.

## Fake binary

`cmd/takt-fake-assistant` реализует сценарии:

- `success`;
- `exit`;
- `timeout`;
- `cancel`;
- `concurrent-output`;
- `malformed-result`;
- invalid version/type/status/unknown fields;
- missing/null/incompatible `exit_code`;
- multiple request/result JSON values;
- OS/envelope exit mismatch;
- negative usage;
- `fresh`;
- `resume`;
- `resume-failed`;
- `session-cycle` для сквозного retry/resume.

Ошибка запуска проверяется отсутствующим бинарником.

## Контрактные проверки

`internal/assistant/contract_test.go` проверяет transport и protocol classification. `internal/runtime/runner_test.go` проверяет `fresh → retry → resume` через настоящий дочерний процесс.

Запуск:

```bash
./scripts/test-fake-assistant.sh
```

Suite также входит в `scripts/verify.sh`.

## Зафиксированная семантика

- OS exit code и envelope `exit_code` обязаны совпадать;
- согласованный ненулевой `exit_code` остаётся `exit`;
- start, timeout и cancel не преобразуются в exit;
- malformed, truncated или несовместимый JSON — `protocol`;
- запрос resume требует `resumed: true` и совпадающий Session ID;
- тихий fallback на fresh запрещён;
- stdout и stderr используют общий thread-safe output budget;
- старый текстовый режим process assistant сохранён.

## Следующий этап

В `v0.1.7-alpha` suite дополнен строгими отрицательными cases. Specialized Pi или OpenCode adapter должен преобразовать собственный API/CLI в тот же контракт и пройти эквивалентный набор тестов. Реальные smoke tests остаются opt-in.
